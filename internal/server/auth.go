package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MarBarb/selfsend/internal/store"
	"golang.org/x/crypto/argon2"
)

const sessionCookieName = "selfsend_session"

type credentials struct {
	Password string `json:"password"`
}

func (a *App) handleSetup(w http.ResponseWriter, r *http.Request) {
	setup, err := a.store.IsSetup(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read instance status")
		return
	}
	if setup {
		writeError(w, http.StatusConflict, "instance is already initialized")
		return
	}
	password, ok := readPassword(w, r)
	if !ok {
		return
	}
	hash, err := hashPassword(password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not secure password")
		return
	}
	if err := a.store.InitializePassword(r.Context(), hash); err != nil {
		writeError(w, http.StatusConflict, "instance is already initialized")
		return
	}
	if err := a.startSession(w, r); err != nil {
		writeError(w, http.StatusInternalServerError, "instance initialized; please sign in")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	password, ok := readPassword(w, r)
	if !ok {
		return
	}
	hash, err := a.store.PasswordHash(r.Context())
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusPreconditionRequired, "instance is not initialized")
		return
	}
	if err != nil || !verifyPassword(password, hash) {
		time.Sleep(250 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "incorrect password")
		return
	}
	if err := a.startSession(w, r); err != nil {
		a.logger.Error("create session", "error", err)
		writeError(w, http.StatusInternalServerError, "could not start session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		_ = a.store.DeleteSession(r.Context(), tokenHash(cookie.Value))
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: a.secureRequest(r),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) startSession(w http.ResponseWriter, r *http.Request) error {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if err := a.store.PurgeExpiredSessions(r.Context(), time.Now()); err != nil {
		a.logger.Warn("purge expired sessions", "error", err)
	}
	if err := a.store.CreateSession(r.Context(), tokenHash(token), expiresAt); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", Expires: expiresAt, MaxAge: 30 * 24 * 60 * 60,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: a.secureRequest(r),
	})
	return nil
}

func (a *App) authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	valid, err := a.store.SessionValid(r.Context(), tokenHash(cookie.Value), time.Now())
	return err == nil && valid
}

func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.authenticated(r) {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next(w, r)
	}
}

func (a *App) secureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return a.config.TrustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func readPassword(w http.ResponseWriter, r *http.Request) (string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var body credentials
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return "", false
	}
	if len(body.Password) < 10 || len(body.Password) > 1024 {
		writeError(w, http.StatusBadRequest, "password must contain at least 10 characters")
		return "", false
	}
	return body.Password, true
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	const memory, iterations, parallelism, keyLength = 64 * 1024, 3, 2, 32
	key := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, keyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, memory, iterations, parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func tokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func parseIntHeader(r *http.Request, name string) (int64, error) {
	value := r.Header.Get(name)
	if value == "" {
		return 0, fmt.Errorf("missing %s", name)
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}
