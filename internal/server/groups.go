package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/MarBarb/selfsend/internal/store"
)

func (a *App) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	var request struct {
		DeviceIDs []string `json:"device_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if len(request.DeviceIDs) < 2 || len(request.DeviceIDs) > 100 {
		writeError(w, http.StatusBadRequest, "select at least two devices")
		return
	}
	for index, deviceID := range request.DeviceIDs {
		request.DeviceIDs[index] = strings.TrimSpace(deviceID)
		if !validDeviceID(request.DeviceIDs[index]) {
			writeError(w, http.StatusBadRequest, "invalid device id")
			return
		}
	}
	groupID, err := randomID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create group")
		return
	}
	conversationID, err := randomID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create group")
		return
	}
	creatorDeviceID, _ := a.currentDeviceID(r)
	conversation, err := a.store.CreateGroup(r.Context(), groupID, conversationID, creatorDeviceID, request.DeviceIDs)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "one or more devices do not exist")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "at least three") {
			writeError(w, http.StatusBadRequest, "select at least two devices")
			return
		}
		a.logger.Error("create group", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create group")
		return
	}
	a.hub.publish("conversations")
	writeJSON(w, http.StatusCreated, conversation)
}
