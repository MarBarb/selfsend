package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MarBarb/selfsend/internal/store"
)

func (a *App) handleCreatePairingInvite(w http.ResponseWriter, r *http.Request) {
	deviceID, _ := a.currentDeviceID(r)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create invitation")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	expiresAt := time.Now().Add(2 * time.Minute)
	if err := a.store.CreatePairingInvite(r.Context(), tokenHash(token), deviceID, expiresAt); err != nil {
		a.logger.Error("create pairing invitation", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create invitation")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "expires_at": expiresAt.UnixMilli()})
}

func (a *App) handleClaimPairing(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 768<<10)
	var request struct {
		Token  string `json:"token"`
		Name   string `json:"name"`
		Avatar string `json:"avatar"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	request.Token = strings.TrimSpace(request.Token)
	request.Name = strings.TrimSpace(request.Name)
	request.Avatar = strings.TrimSpace(request.Avatar)
	if len(request.Token) < 32 || request.Name == "" || utf8.RuneCountInString(request.Name) > 40 {
		writeError(w, http.StatusBadRequest, "invalid invitation or device name")
		return
	}
	if request.Avatar == "" {
		request.Avatar = "设备"
	}
	if len(request.Avatar) > 700<<10 || (!strings.HasPrefix(request.Avatar, "data:image/") && utf8.RuneCountInString(request.Avatar) > 12) {
		writeError(w, http.StatusBadRequest, "invalid avatar")
		return
	}
	deviceID, deviceErr := a.currentDeviceID(r)
	newSession := deviceErr != nil
	if newSession {
		var err error
		deviceID, err = randomID()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not create device")
			return
		}
	} else if device, err := a.store.Device(r.Context(), deviceID); err == nil {
		request.Name, request.Avatar = device.Name, device.Avatar
	}
	result, err := a.store.ClaimPairingInvite(r.Context(), tokenHash(request.Token), deviceID, request.Name, request.Avatar, time.Now())
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusGone, "invitation has expired or was already used")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "itself") {
			writeError(w, http.StatusConflict, "不能添加当前设备自己")
			return
		}
		a.logger.Error("claim pairing invitation", "error", err)
		writeError(w, http.StatusInternalServerError, "could not claim invitation")
		return
	}
	if newSession {
		if err := a.startSession(w, r, result.Device.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "device created; please sign in")
			return
		}
	}
	a.hub.publish("friends")
	writeJSON(w, http.StatusCreated, map[string]any{
		"device": result.Device,
	})
}
