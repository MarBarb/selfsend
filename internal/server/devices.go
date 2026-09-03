package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/MarBarb/selfsend/internal/store"
)

type identityRequest struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

func (a *App) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeIdentityRequest(w, r, true)
	if !ok {
		return
	}
	device, err := a.store.RegisterDevice(r.Context(), request.ID, request.Name, request.Avatar)
	if err != nil {
		a.logger.Error("register device", "error", err)
		writeError(w, http.StatusInternalServerError, "could not register device")
		return
	}
	if boundID, err := a.currentDeviceID(r); err == nil && boundID != device.ID {
		writeError(w, http.StatusConflict, "this session already belongs to another device")
		return
	}
	if err := a.store.BindSessionDevice(r.Context(), tokenHash(a.sessionToken(r)), device.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not bind device session")
		return
	}
	a.hub.publish("devices")
	writeJSON(w, http.StatusOK, device)
}

func (a *App) handleListConversations(w http.ResponseWriter, r *http.Request) {
	deviceID, _ := a.currentDeviceID(r)
	conversations, err := a.store.ListConversations(r.Context(), deviceID)
	if err != nil {
		a.logger.Error("list conversations", "error", err)
		writeError(w, http.StatusInternalServerError, "could not load conversations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": conversations})
}

func (a *App) handleUpdateDevice(w http.ResponseWriter, r *http.Request) {
	deviceID, _ := a.currentDeviceID(r)
	if r.PathValue("id") != deviceID {
		writeError(w, http.StatusForbidden, "a device can only edit its own profile")
		return
	}
	request, ok := decodeIdentityRequest(w, r, false)
	if !ok {
		return
	}
	device, err := a.store.UpdateDevice(r.Context(), r.PathValue("id"), request.Name, request.Avatar)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "device not found")
		return
	}
	if err != nil {
		a.logger.Error("update device", "error", err)
		writeError(w, http.StatusInternalServerError, "could not update device")
		return
	}
	a.hub.publish("devices")
	writeJSON(w, http.StatusOK, device)
}

func decodeIdentityRequest(w http.ResponseWriter, r *http.Request, requireID bool) (identityRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 768<<10)
	var request identityRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return identityRequest{}, false
	}
	request.ID = strings.TrimSpace(request.ID)
	request.Name = strings.TrimSpace(request.Name)
	request.Avatar = strings.TrimSpace(request.Avatar)
	if requireID && !validDeviceID(request.ID) {
		writeError(w, http.StatusBadRequest, "invalid device id")
		return identityRequest{}, false
	}
	if request.Name == "" || utf8.RuneCountInString(request.Name) > 40 {
		writeError(w, http.StatusBadRequest, "name must contain 1 to 40 characters")
		return identityRequest{}, false
	}
	if request.Avatar == "" {
		request.Avatar = "设备"
	}
	if len(request.Avatar) > 700<<10 || (!strings.HasPrefix(request.Avatar, "data:image/") && utf8.RuneCountInString(request.Avatar) > 12) {
		writeError(w, http.StatusBadRequest, "invalid avatar")
		return identityRequest{}, false
	}
	return request, true
}

func validDeviceID(value string) bool {
	if len(value) < 16 || len(value) > 80 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}
