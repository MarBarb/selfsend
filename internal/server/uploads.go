package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MarBarb/selfsend/internal/store"
)

const tusVersion = "1.0.0"

func (a *App) handleUploadOptions(w http.ResponseWriter, _ *http.Request) {
	setTusHeaders(w, a.config.MaxUploadSize)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCreateUpload(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Tus-Resumable") != tusVersion {
		setTusHeaders(w, a.config.MaxUploadSize)
		writeError(w, http.StatusPreconditionFailed, "unsupported resumable upload version")
		return
	}
	totalSize, err := parseIntHeader(r, "Upload-Length")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if totalSize > a.config.MaxUploadSize {
		writeError(w, http.StatusRequestEntityTooLarge, "file exceeds the configured upload limit")
		return
	}
	metadata, err := parseUploadMetadata(r.Header.Get("Upload-Metadata"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid upload metadata")
		return
	}
	fileName := cleanFileName(metadata["filename"])
	if fileName == "" {
		writeError(w, http.StatusBadRequest, "filename metadata is required")
		return
	}
	mimeType := strings.TrimSpace(metadata["filetype"])
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if _, _, err := mime.ParseMediaType(mimeType); err != nil {
		mimeType = "application/octet-stream"
	}
	lastModified, _ := strconv.ParseInt(metadata["lastmodified"], 10, 64)
	id, err := randomID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create upload")
		return
	}
	tempPath := filepath.Join(a.config.DataDir, "uploads", id+".part")
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		a.logger.Error("create upload file", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create upload")
		return
	}
	file.Close()
	upload := store.Upload{ID: id, FileName: fileName, MimeType: mimeType, TotalSize: totalSize, TempPath: tempPath, LastModified: lastModified}
	if err := a.store.CreateUpload(r.Context(), upload); err != nil {
		os.Remove(tempPath)
		writeError(w, http.StatusInternalServerError, "could not create upload")
		return
	}
	location := "/api/uploads/" + id
	setTusHeaders(w, a.config.MaxUploadSize)
	w.Header().Set("Location", location)
	w.Header().Set("Upload-Offset", "0")
	if totalSize == 0 {
		if err := a.finalizeUpload(r.Context(), upload); err != nil {
			writeError(w, http.StatusInternalServerError, "could not finalize upload")
			return
		}
	}
	w.WriteHeader(http.StatusCreated)
}

func (a *App) handleUploadHead(w http.ResponseWriter, r *http.Request) {
	upload, err := a.store.Upload(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "upload not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read upload")
		return
	}
	setTusHeaders(w, a.config.MaxUploadSize)
	w.Header().Set("Upload-Length", strconv.FormatInt(upload.TotalSize, 10))
	w.Header().Set("Upload-Offset", strconv.FormatInt(upload.Offset, 10))
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleUploadPatch(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Tus-Resumable") != tusVersion {
		setTusHeaders(w, a.config.MaxUploadSize)
		writeError(w, http.StatusPreconditionFailed, "unsupported resumable upload version")
		return
	}
	if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mediaType != "application/offset+octet-stream" {
		writeError(w, http.StatusUnsupportedMediaType, "expected application/offset+octet-stream")
		return
	}
	requestedOffset, err := parseIntHeader(r, "Upload-Offset")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if r.ContentLength < 0 {
		writeError(w, http.StatusLengthRequired, "Content-Length is required")
		return
	}

	a.uploadsMu.Lock()
	defer a.uploadsMu.Unlock()
	upload, err := a.store.Upload(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "upload not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read upload")
		return
	}
	if requestedOffset != upload.Offset {
		setTusHeaders(w, a.config.MaxUploadSize)
		w.Header().Set("Upload-Offset", strconv.FormatInt(upload.Offset, 10))
		writeError(w, http.StatusConflict, "upload offset does not match")
		return
	}
	if r.ContentLength > upload.TotalSize-upload.Offset {
		writeError(w, http.StatusRequestEntityTooLarge, "chunk exceeds remaining upload size")
		return
	}
	file, err := os.OpenFile(upload.TempPath, os.O_WRONLY, 0o600)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not open upload")
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.Size() != upload.Offset {
		writeError(w, http.StatusConflict, "stored upload offset is inconsistent")
		return
	}
	if _, err := file.Seek(upload.Offset, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "could not seek upload")
		return
	}
	written, copyErr := io.CopyN(file, r.Body, r.ContentLength)
	if copyErr != nil || written != r.ContentLength {
		_ = file.Truncate(upload.Offset)
		writeError(w, http.StatusBadRequest, "upload chunk ended unexpectedly")
		return
	}
	if err := file.Sync(); err != nil {
		_ = file.Truncate(upload.Offset)
		writeError(w, http.StatusInternalServerError, "could not persist upload chunk")
		return
	}
	nextOffset := upload.Offset + written
	if err := a.store.UpdateUploadOffset(r.Context(), upload.ID, upload.Offset, nextOffset); err != nil {
		_ = file.Truncate(upload.Offset)
		writeError(w, http.StatusConflict, "upload changed concurrently")
		return
	}
	upload.Offset = nextOffset
	if nextOffset == upload.TotalSize {
		file.Close()
		if err := a.finalizeUpload(r.Context(), upload); err != nil {
			a.logger.Error("finalize upload", "id", upload.ID, "error", err)
			writeError(w, http.StatusInternalServerError, "could not finalize upload")
			return
		}
	}
	setTusHeaders(w, a.config.MaxUploadSize)
	w.Header().Set("Upload-Offset", strconv.FormatInt(nextOffset, 10))
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleDeleteUpload(w http.ResponseWriter, r *http.Request) {
	tempPath, err := a.store.DeleteUpload(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "upload not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not cancel upload")
		return
	}
	_ = os.Remove(tempPath)
	setTusHeaders(w, a.config.MaxUploadSize)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) finalizeUpload(ctx context.Context, upload store.Upload) error {
	file, err := os.Open(upload.TempPath)
	if err != nil {
		return err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	blobDir := filepath.Join(a.config.DataDir, "blobs", upload.ID)
	if err := os.MkdirAll(blobDir, 0o750); err != nil {
		return err
	}
	blobPath := filepath.Join(blobDir, "data")
	if err := os.Rename(upload.TempPath, blobPath); err != nil {
		return err
	}
	if err := a.store.CompleteUpload(ctx, upload, hex.EncodeToString(hash.Sum(nil)), blobPath); err != nil {
		_ = os.Rename(blobPath, upload.TempPath)
		return err
	}
	a.hub.publish("timeline")
	return nil
}

func (a *App) handleDownload(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.Item(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read file")
		return
	}
	file, err := os.Open(item.StoragePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file data is missing")
		return
	}
	defer file.Close()
	disposition := "attachment"
	if r.URL.Query().Get("inline") == "1" {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", item.MimeType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": item.FileName}))
	w.Header().Set("ETag", `"sha256-`+item.SHA256+`"`)
	http.ServeContent(w, r, item.FileName, time.UnixMilli(item.CreatedAt), file)
}

func parseUploadMetadata(header string) (map[string]string, error) {
	metadata := make(map[string]string)
	if strings.TrimSpace(header) == "" {
		return metadata, nil
	}
	for _, pair := range strings.Split(header, ",") {
		parts := strings.SplitN(strings.TrimSpace(pair), " ", 2)
		if len(parts) != 2 || parts[0] == "" {
			return nil, errors.New("invalid metadata pair")
		}
		decoded, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil || !utf8.Valid(decoded) {
			return nil, errors.New("invalid metadata value")
		}
		metadata[strings.ToLower(parts[0])] = string(decoded)
	}
	return metadata, nil
}

func cleanFileName(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\\", "/"), "\x00", ""))
	value = filepath.Base(value)
	if value == "." || value == "/" || value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) > 255 {
		value = string(runes[:255])
	}
	return value
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func setTusHeaders(w http.ResponseWriter, maxSize int64) {
	w.Header().Set("Tus-Resumable", tusVersion)
	w.Header().Set("Tus-Version", tusVersion)
	w.Header().Set("Tus-Extension", "creation,termination")
	w.Header().Set("Tus-Max-Size", fmt.Sprintf("%d", maxSize))
}
