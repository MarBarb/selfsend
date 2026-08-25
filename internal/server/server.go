package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/MarBarb/selfsend/internal/store"
	webassets "github.com/MarBarb/selfsend/internal/web"
)

type Config struct {
	ListenAddr    string
	DataDir       string
	AdminPassword string
	MaxUploadSize int64
	TrustProxy    bool
	Version       string
}

type App struct {
	config    Config
	store     *store.Store
	logger    *slog.Logger
	hub       *eventHub
	uploadsMu sync.Mutex
	web       http.Handler
}

func New(config Config, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if config.DataDir == "" {
		config.DataDir = "./data"
	}
	if config.ListenAddr == "" {
		config.ListenAddr = ":8080"
	}
	if config.MaxUploadSize <= 0 {
		config.MaxUploadSize = 20 << 30
	}
	db, err := store.Open(config.DataDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(pathFor(config.DataDir, "uploads"), 0o750); err != nil {
		db.Close()
		return nil, fmt.Errorf("create uploads directory: %w", err)
	}
	if err := os.MkdirAll(pathFor(config.DataDir, "blobs"), 0o750); err != nil {
		db.Close()
		return nil, fmt.Errorf("create blobs directory: %w", err)
	}

	app := &App{config: config, store: db, logger: logger, hub: newEventHub()}
	app.web, err = staticHandler()
	if err != nil {
		db.Close()
		return nil, err
	}

	if config.AdminPassword != "" {
		setup, err := db.IsSetup(context.Background())
		if err != nil {
			db.Close()
			return nil, err
		}
		if !setup {
			hash, err := hashPassword(config.AdminPassword)
			if err != nil {
				db.Close()
				return nil, err
			}
			if err := db.InitializePassword(context.Background(), hash); err != nil {
				db.Close()
				return nil, err
			}
			logger.Info("instance initialized from SELFSEND_ADMIN_PASSWORD")
		}
	}
	return app, nil
}

func (a *App) Close() error { return a.store.Close() }

func Run(config Config, logger *slog.Logger) error {
	app, err := New(config, logger)
	if err != nil {
		return err
	}
	defer app.Close()

	httpServer := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("SelfSend is ready", "address", config.ListenAddr, "data", config.DataDir, "version", config.Version)
		serverErr <- httpServer.ListenAndServe()
	}()

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-shutdownCtx.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(ctx)
	}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.handleHealth)
	mux.HandleFunc("GET /api/status", a.handleStatus)
	mux.HandleFunc("POST /api/setup", a.handleSetup)
	mux.HandleFunc("POST /api/login", a.handleLogin)
	mux.HandleFunc("POST /api/logout", a.requireAuth(a.handleLogout))
	mux.HandleFunc("GET /api/items", a.requireAuth(a.handleListItems))
	mux.HandleFunc("DELETE /api/items/{id}", a.requireAuth(a.handleDeleteItem))
	mux.HandleFunc("GET /api/files/{id}", a.requireAuth(a.handleDownload))
	mux.HandleFunc("OPTIONS /api/uploads", a.requireAuth(a.handleUploadOptions))
	mux.HandleFunc("POST /api/uploads", a.requireAuth(a.handleCreateUpload))
	mux.HandleFunc("HEAD /api/uploads/{id}", a.requireAuth(a.handleUploadHead))
	mux.HandleFunc("PATCH /api/uploads/{id}", a.requireAuth(a.handleUploadPatch))
	mux.HandleFunc("DELETE /api/uploads/{id}", a.requireAuth(a.handleDeleteUpload))
	mux.HandleFunc("GET /api/events", a.requireAuth(a.handleEvents))
	mux.Handle("/", a.web)
	return a.securityHeaders(a.originGuard(a.accessLog(mux)))
}

func (a *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": a.config.Version})
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	setup, err := a.store.IsSetup(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read instance status")
		return
	}
	authenticated := setup && a.authenticated(r)
	response := map[string]any{
		"setup_required":  !setup,
		"authenticated":   authenticated,
		"max_upload_size": a.config.MaxUploadSize,
		"version":         a.config.Version,
	}
	if authenticated {
		count, size, err := a.store.Stats(r.Context())
		if err == nil {
			response["item_count"] = count
			response["total_bytes"] = size
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) handleListItems(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	var before int64
	if raw := r.URL.Query().Get("before"); raw != "" {
		before, _ = strconv.ParseInt(raw, 10, 64)
	}
	items, err := a.store.ListItems(r.Context(), before, limit)
	if err != nil {
		a.logger.Error("list items", "error", err)
		writeError(w, http.StatusInternalServerError, "could not load timeline")
		return
	}
	var nextCursor int64
	if len(items) == limit {
		nextCursor = items[len(items)-1].CreatedAt
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nextCursor})
}

func (a *App) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	storagePath, err := a.store.DeleteItem(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		a.logger.Error("delete item", "error", err)
		writeError(w, http.StatusInternalServerError, "could not delete file")
		return
	}
	if err := os.RemoveAll(pathDir(storagePath)); err != nil {
		a.logger.Warn("remove blob directory", "path", storagePath, "error", err)
	}
	a.hub.publish("timeline")
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusNotImplemented, "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	events, unsubscribe := a.hub.subscribe()
	defer unsubscribe()
	fmt.Fprint(w, "event: ready\ndata: connected\n\n")
	flusher.Flush()
	keepAlive := time.NewTicker(25 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case event := <-events:
			fmt.Fprintf(w, "event: %s\ndata: {}\n\n", event)
			flusher.Flush()
		case <-keepAlive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (a *App) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self' ws: wss:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'")
		next.ServeHTTP(w, r)
	})
}

func (a *App) originGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
				writeError(w, http.StatusForbidden, "cross-origin request rejected")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/health" {
			a.logger.Debug("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
		}
	})
}

func staticHandler() (http.Handler, error) {
	root, err := fs.Sub(webassets.Assets, "dist")
	if err != nil {
		return nil, fmt.Errorf("open embedded web assets: %w", err)
	}
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if requested == "." || requested == "" {
			serveIndex(w, root)
			return
		}
		if requested == "index.html" {
			serveIndex(w, root)
			return
		}
		if _, err := fs.Stat(root, requested); err == nil {
			requestCopy := r.Clone(r.Context())
			requestCopy.URL.Path = "/" + requested
			files.ServeHTTP(w, requestCopy)
			return
		}
		serveIndex(w, root)
	}), nil
}

func serveIndex(w http.ResponseWriter, root fs.FS) {
	index, err := fs.ReadFile(root, "index.html")
	if err != nil {
		http.Error(w, "web client is not built", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(index)
}

type eventHub struct {
	mu          sync.Mutex
	subscribers map[chan string]struct{}
}

func newEventHub() *eventHub { return &eventHub{subscribers: make(map[chan string]struct{})} }

func (h *eventHub) subscribe() (<-chan string, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	channel := make(chan string, 4)
	h.subscribers[channel] = struct{}{}
	return channel, func() {
		h.mu.Lock()
		delete(h.subscribers, channel)
		close(channel)
		h.mu.Unlock()
	}
}

func (h *eventHub) publish(event string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for subscriber := range h.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func pathFor(parts ...string) string { return filepath.Join(parts...) }
func pathDir(filePath string) string { return filepath.Dir(filePath) }

func sameOrigin(origin, host string) bool {
	origin = strings.TrimSuffix(origin, "/")
	return origin == "http://"+host || origin == "https://"+host
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
