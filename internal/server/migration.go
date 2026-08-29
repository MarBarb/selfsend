package server

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MarBarb/selfsend/internal/store"
	"golang.org/x/sys/unix"
)

const migrationChunkSize = 4 << 20

type MigrationJob struct {
	ID          string `json:"id,omitempty"`
	State       string `json:"state"`
	Stage       string `json:"stage,omitempty"`
	TargetURL   string `json:"target_url,omitempty"`
	TargetName  string `json:"target_name,omitempty"`
	TotalBytes  int64  `json:"total_bytes,omitempty"`
	SentBytes   int64  `json:"sent_bytes,omitempty"`
	Files       int    `json:"files,omitempty"`
	Error       string `json:"error,omitempty"`
	StartedAt   int64  `json:"started_at,omitempty"`
	CompletedAt int64  `json:"completed_at,omitempty"`
}

type receiverState struct {
	ID             string `json:"id"`
	TokenHash      string `json:"token_hash"`
	State          string `json:"state"`
	BaseURL        string `json:"base_url"`
	HostName       string `json:"host_name"`
	HostAvatar     string `json:"host_avatar"`
	ExpiresAt      int64  `json:"expires_at"`
	ExpectedSize   int64  `json:"expected_size,omitempty"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
	Offset         int64  `json:"offset,omitempty"`
	InstanceID     string `json:"instance_id,omitempty"`
	HostDeviceID   string `json:"host_device_id,omitempty"`
	Error          string `json:"error,omitempty"`
}

type archiveManifest struct {
	Format     int                   `json:"format"`
	InstanceID string                `json:"instance_id"`
	CreatedAt  int64                 `json:"created_at"`
	Files      []archiveManifestFile `json:"files"`
}

type archiveManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type handoffPayload struct {
	InstanceID string `json:"instance_id"`
	DeviceID   string `json:"device_id"`
	Nonce      string `json:"nonce"`
	ExpiresAt  int64  `json:"expires_at"`
}

func (a *App) handleServerInfo(w http.ResponseWriter, r *http.Request) {
	info, err := a.store.ServerInfo(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read server information")
		return
	}
	if info.CanonicalURL == "" {
		info.CanonicalURL = requestBaseURL(r)
	}
	count, size, _ := a.store.Stats(r.Context())
	pending, _ := a.store.PendingUploadCount(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"server": info, "item_count": count, "total_bytes": size, "pending_uploads": pending, "version": a.config.Version})
}

func (a *App) handlePrepareBackup(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if json.NewDecoder(r.Body).Decode(&request) != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	passwordHash, err := a.store.PasswordHash(r.Context())
	if err != nil || !verifyPassword(request.Password, passwordHash) {
		writeError(w, http.StatusUnauthorized, "管理员密码不正确")
		return
	}
	pending, _ := a.store.PendingUploadCount(r.Context())
	if pending != 0 {
		writeError(w, http.StatusConflict, "请先等待或取消尚未完成的文件上传")
		return
	}
	if !a.maintenanceMu.TryLock() {
		writeError(w, http.StatusConflict, "another backup or migration is already running")
		return
	}
	defer a.maintenanceMu.Unlock()
	if err := a.store.SetServerState(r.Context(), store.ServerStateMigrating, ""); err != nil {
		writeError(w, http.StatusInternalServerError, "could not prepare backup")
		return
	}
	a.hub.publish("server")
	a.uploadsMu.Lock()
	id, _ := randomID()
	archivePath, _, _, archiveErr := a.createMigrationArchive(r.Context(), id)
	a.uploadsMu.Unlock()
	_ = a.store.SetServerState(context.Background(), store.ServerStateActive, "")
	a.hub.publish("server")
	if archiveErr != nil {
		writeError(w, http.StatusInternalServerError, "创建备份失败")
		return
	}
	token, _ := randomID()
	a.migrationMu.Lock()
	a.backups[token] = archivePath
	a.migrationMu.Unlock()
	time.AfterFunc(30*time.Minute, func() {
		a.migrationMu.Lock()
		path, exists := a.backups[token]
		if exists {
			delete(a.backups, token)
		}
		a.migrationMu.Unlock()
		if exists {
			_ = os.Remove(path)
		}
	})
	writeJSON(w, http.StatusCreated, map[string]string{"download_url": "/api/server/backups/" + token})
}

func (a *App) handleDownloadPreparedBackup(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	a.migrationMu.Lock()
	archivePath, exists := a.backups[token]
	if exists {
		delete(a.backups, token)
	}
	a.migrationMu.Unlock()
	if !exists {
		writeError(w, http.StatusNotFound, "backup is no longer available")
		return
	}
	defer os.Remove(archivePath)
	file, err := os.Open(archivePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not open backup")
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read backup")
		return
	}
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", `attachment; filename="selfsend-backup-`+time.Now().Format("20060102-150405")+`.tar"`)
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	http.ServeContent(w, r, filepath.Base(archivePath), stat.ModTime(), file)
}

func (a *App) handleCreateReceiver(w http.ResponseWriter, r *http.Request) {
	setup, err := a.store.IsSetup(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not inspect receiver")
		return
	}
	if setup {
		writeError(w, http.StatusConflict, "only a new empty SelfSend server can receive a migration")
		return
	}
	if existing, err := loadReceiver(a.config.DataDir); err == nil && existing.State != "error" && existing.Offset > 0 && time.Now().Unix() <= existing.ExpiresAt {
		writeError(w, http.StatusConflict, "a migration receiver is already active on this server")
		return
	}
	var request struct {
		Name   string `json:"name"`
		Avatar string `json:"avatar"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" || len([]rune(request.Name)) > 40 {
		writeError(w, http.StatusBadRequest, "server name is required")
		return
	}
	if request.Avatar == "" {
		request.Avatar = "💻"
	}
	id, _ := randomID()
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create migration receiver")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	state := receiverState{
		ID: id, TokenHash: tokenDigest(token), State: "waiting", BaseURL: requestBaseURL(r), HostName: request.Name,
		HostAvatar: request.Avatar, ExpiresAt: time.Now().Add(30 * time.Minute).Unix(),
	}
	if err := saveReceiver(a.config.DataDir, state); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save migration receiver")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "token": token, "expires_at": state.ExpiresAt,
		"offer_url": state.BaseURL + "/#receive=" + url.QueryEscape(token),
	})
}

func (a *App) handleReceiverStatus(w http.ResponseWriter, r *http.Request) {
	state, ok := a.authorizedReceiver(w, r)
	if !ok {
		return
	}
	if (state.State == "waiting" || state.State == "uploading") && state.BaseURL != requestBaseURL(r) {
		state.BaseURL = requestBaseURL(r)
		_ = saveReceiver(a.config.DataDir, state)
	}
	state.TokenHash = ""
	writeJSON(w, http.StatusOK, state)
}

func (a *App) handleInitReceiverArchive(w http.ResponseWriter, r *http.Request) {
	state, ok := a.authorizedReceiver(w, r)
	if !ok {
		return
	}
	var request struct {
		Size       int64  `json:"size"`
		SHA256     string `json:"sha256"`
		InstanceID string `json:"instance_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Size <= 0 || (request.SHA256 != "" && len(request.SHA256) != 64) {
		writeError(w, http.StatusBadRequest, "invalid migration archive")
		return
	}
	if available, err := availableDisk(a.config.DataDir); err == nil && available < uint64(request.Size)*2+(64<<20) {
		writeError(w, http.StatusInsufficientStorage, "the new server does not have enough free space")
		return
	}
	a.migrationMu.Lock()
	defer a.migrationMu.Unlock()
	archivePath := incomingArchivePath(a.config.DataDir)
	if state.ExpectedSHA256 != request.SHA256 || state.ExpectedSize != request.Size {
		_ = os.Remove(archivePath)
		state.Offset = 0
	}
	state.State = "uploading"
	state.ExpiresAt = time.Now().Add(24 * time.Hour).Unix()
	state.ExpectedSize = request.Size
	state.ExpectedSHA256 = request.SHA256
	state.InstanceID = request.InstanceID
	if stat, err := os.Stat(archivePath); err == nil {
		state.Offset = stat.Size()
	}
	if err := saveReceiver(a.config.DataDir, state); err != nil {
		writeError(w, http.StatusInternalServerError, "could not initialize migration")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"offset": state.Offset})
}

func (a *App) handleReceiverArchiveHead(w http.ResponseWriter, r *http.Request) {
	state, ok := a.authorizedReceiver(w, r)
	if !ok {
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(state.Offset, 10))
	w.Header().Set("Upload-Length", strconv.FormatInt(state.ExpectedSize, 10))
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleReceiverArchivePatch(w http.ResponseWriter, r *http.Request) {
	state, ok := a.authorizedReceiver(w, r)
	if !ok {
		return
	}
	offset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	encrypted := r.Header.Get("X-SelfSend-Migration-Encrypted") == "1"
	plainLength := r.ContentLength
	if encrypted {
		plainLength, err = strconv.ParseInt(r.Header.Get("X-SelfSend-Plain-Length"), 10, 64)
	}
	if err != nil || offset != state.Offset || plainLength < 0 || plainLength > migrationChunkSize || offset+plainLength > state.ExpectedSize || (!encrypted && r.ContentLength != plainLength) || (encrypted && r.ContentLength != plainLength+16) {
		w.Header().Set("Upload-Offset", strconv.FormatInt(state.Offset, 10))
		writeError(w, http.StatusConflict, "migration archive offset does not match")
		return
	}
	a.migrationMu.Lock()
	defer a.migrationMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(incomingArchivePath(a.config.DataDir)), 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create migration staging directory")
		return
	}
	file, err := os.OpenFile(incomingArchivePath(a.config.DataDir), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not open migration archive")
		return
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "could not seek migration archive")
		return
	}
	var payload io.Reader = io.LimitReader(r.Body, r.ContentLength)
	if encrypted {
		ciphertext, readErr := io.ReadAll(payload)
		if readErr != nil {
			_ = file.Truncate(offset)
			writeError(w, http.StatusBadRequest, "migration chunk ended unexpectedly")
			return
		}
		plaintext, decryptErr := decryptMigrationChunk(state.TokenHash, offset, ciphertext)
		if decryptErr != nil || int64(len(plaintext)) != plainLength {
			_ = file.Truncate(offset)
			writeError(w, http.StatusBadRequest, "migration chunk authentication failed")
			return
		}
		payload = bytes.NewReader(plaintext)
	}
	written, err := io.CopyN(file, payload, plainLength)
	if err != nil || written != plainLength || file.Sync() != nil {
		_ = file.Truncate(offset)
		writeError(w, http.StatusBadRequest, "migration chunk ended unexpectedly")
		return
	}
	state.Offset += plainLength
	if err := saveReceiver(a.config.DataDir, state); err != nil {
		writeError(w, http.StatusInternalServerError, "could not persist migration progress")
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(state.Offset, 10))
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleActivateReceiver(w http.ResponseWriter, r *http.Request) {
	state, ok := a.authorizedReceiver(w, r)
	if !ok {
		return
	}
	if state.Offset != state.ExpectedSize || state.ExpectedSize <= 0 {
		writeError(w, http.StatusConflict, "migration archive is incomplete")
		return
	}
	if state.ExpectedSHA256 != "" {
		digest, err := hashFile(incomingArchivePath(a.config.DataDir))
		if err != nil || digest != state.ExpectedSHA256 {
			writeError(w, http.StatusUnprocessableEntity, "migration archive checksum failed")
			return
		}
	}
	state.State = "restarting"
	if err := saveReceiver(a.config.DataDir, state); err != nil {
		writeError(w, http.StatusInternalServerError, "could not activate migration")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"restarting": true})
	if a.config.restart != nil {
		time.AfterFunc(250*time.Millisecond, a.config.restart)
	}
}

func (a *App) authorizedReceiver(w http.ResponseWriter, r *http.Request) (receiverState, bool) {
	state, err := loadReceiver(a.config.DataDir)
	if err != nil {
		writeError(w, http.StatusNotFound, "migration receiver is not active")
		return receiverState{}, false
	}
	if !validReceiverAuthorization(r, state.TokenHash) || time.Now().Unix() > state.ExpiresAt {
		writeError(w, http.StatusUnauthorized, "invalid or expired migration token")
		return receiverState{}, false
	}
	return state, true
}

func (a *App) handleStartMigration(w http.ResponseWriter, r *http.Request) {
	var request struct {
		TargetURL string `json:"target_url"`
		Token     string `json:"token"`
		Password  string `json:"password"`
		Mode      string `json:"mode"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	passwordHash, err := a.store.PasswordHash(r.Context())
	if err != nil || !verifyPassword(request.Password, passwordHash) {
		writeError(w, http.StatusUnauthorized, "管理员密码不正确")
		return
	}
	targetURL, err := validateMigrationTarget(request.TargetURL, request.Mode)
	if err != nil || request.Token == "" {
		if request.Mode == "online" {
			writeError(w, http.StatusBadRequest, "请输入可从公网访问的 HTTPS 迁移地址")
		} else {
			writeError(w, http.StatusBadRequest, "请输入有效的局域网迁移地址")
		}
		return
	}
	if !a.maintenanceMu.TryLock() {
		writeError(w, http.StatusConflict, "another backup or migration is already running")
		return
	}
	a.migrationMu.Lock()
	if a.migrationJob.State == "running" {
		a.migrationMu.Unlock()
		a.maintenanceMu.Unlock()
		writeError(w, http.StatusConflict, "a server migration is already running")
		return
	}
	id, _ := randomID()
	a.migrationJob = MigrationJob{ID: id, State: "running", Stage: "连接新服务器", TargetURL: targetURL, StartedAt: time.Now().UnixMilli()}
	a.migrationMu.Unlock()
	go a.runMigration(id, targetURL, request.Token)
	writeJSON(w, http.StatusAccepted, a.currentMigrationJob())
}

func (a *App) handleMigrationStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.currentMigrationJob())
}

func (a *App) currentMigrationJob() MigrationJob {
	a.migrationMu.Lock()
	defer a.migrationMu.Unlock()
	return a.migrationJob
}

func (a *App) updateMigration(id string, update func(*MigrationJob)) {
	a.migrationMu.Lock()
	defer a.migrationMu.Unlock()
	if a.migrationJob.ID == id {
		update(&a.migrationJob)
	}
}

func (a *App) runMigration(id, targetURL, token string) {
	defer a.maintenanceMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	defer cancel()
	cutoverStarted := false
	fail := func(err error) {
		if !cutoverStarted {
			_ = a.store.SetServerState(context.Background(), store.ServerStateActive, "")
		}
		a.updateMigration(id, func(job *MigrationJob) {
			job.State = "failed"
			job.Error = err.Error()
			if cutoverStarted {
				job.Error += "。为避免两台服务器同时写入，旧服务器保持只读；请确认新服务器状态后再决定是否恢复旧服务器。"
			}
			job.CompletedAt = time.Now().UnixMilli()
		})
		a.hub.publish("server")
	}
	receiver, err := fetchReceiver(ctx, targetURL, token)
	if err != nil {
		fail(err)
		return
	}
	a.updateMigration(id, func(job *MigrationJob) { job.TargetName = receiver.HostName; job.Stage = "检查现有数据" })
	if receiver.State != "waiting" && receiver.State != "uploading" {
		fail(errors.New("新服务器当前不能接收迁移"))
		return
	}
	pending, err := a.store.PendingUploadCount(ctx)
	if err != nil || pending > 0 {
		fail(errors.New("请先等待或取消尚未完成的文件上传"))
		return
	}
	if err := a.store.IntegrityCheck(ctx); err != nil {
		fail(fmt.Errorf("数据库检查失败：%w", err))
		return
	}
	if err := a.store.SetServerState(ctx, store.ServerStateMigrating, targetURL); err != nil {
		fail(err)
		return
	}
	a.hub.publish("server")
	a.uploadsMu.Lock()
	archivePath, manifest, archiveSHA, err := a.createMigrationArchive(ctx, id)
	a.uploadsMu.Unlock()
	if err != nil {
		fail(err)
		return
	}
	defer os.Remove(archivePath)
	stat, _ := os.Stat(archivePath)
	a.updateMigration(id, func(job *MigrationJob) {
		job.Stage = "传输数据"
		job.TotalBytes = stat.Size()
		job.Files = len(manifest.Files)
	})
	offset, err := initializeRemoteArchive(ctx, targetURL, token, stat.Size(), archiveSHA, manifest.InstanceID)
	if err != nil {
		fail(err)
		return
	}
	if err := a.uploadMigrationArchive(ctx, id, targetURL, token, archivePath, offset, stat.Size()); err != nil {
		fail(err)
		return
	}
	a.updateMigration(id, func(job *MigrationJob) { job.Stage = "校验并启动新服务器" })
	if err := a.store.SetServerState(ctx, store.ServerStateRetired, targetURL); err != nil {
		fail(err)
		return
	}
	cutoverStarted = true
	if err := activateRemote(ctx, targetURL, token); err != nil {
		fail(err)
		return
	}
	activeReceiver, err := waitForReceiverActive(ctx, targetURL, token)
	if err != nil {
		fail(err)
		return
	}
	if activeReceiver.BaseURL != "" {
		_ = a.store.SetServerState(ctx, store.ServerStateRetired, activeReceiver.BaseURL)
	}
	a.updateMigration(id, func(job *MigrationJob) {
		job.State = "completed"
		job.Stage = "迁移完成"
		job.SentBytes = job.TotalBytes
		job.CompletedAt = time.Now().UnixMilli()
	})
	a.hub.publish("server-moved")
}

func (a *App) handleRollbackMigration(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if json.NewDecoder(r.Body).Decode(&request) != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	passwordHash, err := a.store.PasswordHash(r.Context())
	if err != nil || !verifyPassword(request.Password, passwordHash) {
		writeError(w, http.StatusUnauthorized, "管理员密码不正确")
		return
	}
	info, err := a.store.ServerInfo(r.Context())
	if err != nil || info.State != store.ServerStateRetired {
		writeError(w, http.StatusConflict, "the old server is not in retired state")
		return
	}
	if err := a.store.SetServerState(r.Context(), store.ServerStateActive, ""); err != nil {
		writeError(w, http.StatusInternalServerError, "could not restore old server")
		return
	}
	a.updateMigration(a.currentMigrationJob().ID, func(job *MigrationJob) { job.State = "failed"; job.Stage = "旧服务器已恢复"; job.Error = "" })
	a.hub.publish("server")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) createMigrationArchive(ctx context.Context, id string) (string, archiveManifest, string, error) {
	a.updateMigration(id, func(job *MigrationJob) { job.Stage = "创建一致性快照" })
	if err := a.store.Checkpoint(ctx); err != nil {
		return "", archiveManifest{}, "", err
	}
	info, err := a.store.ServerInfo(ctx)
	if err != nil {
		return "", archiveManifest{}, "", err
	}
	dir := filepath.Join(a.config.DataDir, "migration")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", archiveManifest{}, "", err
	}
	snapshot := filepath.Join(dir, "snapshot-"+id+".db")
	if err := copyFile(filepath.Join(a.config.DataDir, "selfsend.db"), snapshot); err != nil {
		return "", archiveManifest{}, "", err
	}
	defer os.Remove(snapshot)
	archivePath := filepath.Join(dir, "outgoing-"+id+".tar")
	manifest := archiveManifest{Format: 1, InstanceID: info.InstanceID, CreatedAt: time.Now().UnixMilli()}
	inputs := []struct{ source, name string }{{snapshot, "selfsend.db"}}
	if err := filepath.WalkDir(filepath.Join(a.config.DataDir, "blobs"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		relative, err := filepath.Rel(a.config.DataDir, path)
		if err != nil {
			return err
		}
		inputs = append(inputs, struct{ source, name string }{path, filepath.ToSlash(relative)})
		return nil
	}); err != nil {
		return "", archiveManifest{}, "", err
	}
	output, err := os.OpenFile(archivePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", archiveManifest{}, "", err
	}
	tarWriter := tar.NewWriter(output)
	for _, input := range inputs {
		stat, err := os.Stat(input.source)
		if err != nil {
			tarWriter.Close()
			output.Close()
			return "", archiveManifest{}, "", err
		}
		digest, err := hashFile(input.source)
		if err != nil {
			tarWriter.Close()
			output.Close()
			return "", archiveManifest{}, "", err
		}
		manifest.Files = append(manifest.Files, archiveManifestFile{Path: input.name, Size: stat.Size(), SHA256: digest})
		header := &tar.Header{Name: input.name, Mode: 0o600, Size: stat.Size(), ModTime: stat.ModTime()}
		if err := tarWriter.WriteHeader(header); err != nil {
			tarWriter.Close()
			output.Close()
			return "", archiveManifest{}, "", err
		}
		file, err := os.Open(input.source)
		if err != nil {
			tarWriter.Close()
			output.Close()
			return "", archiveManifest{}, "", err
		}
		_, copyErr := io.Copy(tarWriter, file)
		file.Close()
		if copyErr != nil {
			tarWriter.Close()
			output.Close()
			return "", archiveManifest{}, "", copyErr
		}
	}
	manifestJSON, _ := json.Marshal(manifest)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(manifestJSON)), ModTime: time.Now()}); err != nil {
		tarWriter.Close()
		output.Close()
		return "", archiveManifest{}, "", err
	}
	if _, err := tarWriter.Write(manifestJSON); err != nil {
		tarWriter.Close()
		output.Close()
		return "", archiveManifest{}, "", err
	}
	if err := tarWriter.Close(); err != nil {
		output.Close()
		return "", archiveManifest{}, "", err
	}
	if err := output.Sync(); err != nil {
		output.Close()
		return "", archiveManifest{}, "", err
	}
	if err := output.Close(); err != nil {
		return "", archiveManifest{}, "", err
	}
	digest, err := hashFile(archivePath)
	return archivePath, manifest, digest, err
}

func (a *App) uploadMigrationArchive(ctx context.Context, id, targetURL, token, archivePath string, offset, total int64) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	buffer := make([]byte, migrationChunkSize)
	for offset < total {
		count, readErr := io.ReadFull(file, buffer)
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			return readErr
		}
		ciphertext, err := encryptMigrationChunk(tokenDigest(token), offset, buffer[:count])
		if err != nil {
			return err
		}
		request, _ := http.NewRequestWithContext(ctx, http.MethodPatch, targetURL+"/api/migration/receivers/current/archive", bytes.NewReader(ciphertext))
		setReceiverAuthorization(request, token)
		request.Header.Set("Content-Type", "application/offset+octet-stream")
		request.Header.Set("Upload-Offset", strconv.FormatInt(offset, 10))
		request.Header.Set("X-SelfSend-Migration-Encrypted", "1")
		request.Header.Set("X-SelfSend-Plain-Length", strconv.Itoa(count))
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("连接新服务器失败：%w", err)
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			return fmt.Errorf("新服务器拒绝数据分片：%s", responseError(body, response.StatusCode))
		}
		offset += int64(count)
		a.updateMigration(id, func(job *MigrationJob) { job.SentBytes = offset })
	}
	return nil
}

func fetchReceiver(ctx context.Context, targetURL, token string) (receiverState, error) {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, targetURL+"/api/migration/receivers/current", nil)
	setReceiverAuthorization(request, token)
	response, err := (&http.Client{Timeout: 8 * time.Second}).Do(request)
	if err != nil {
		return receiverState{}, fmt.Errorf("无法连接新服务器：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return receiverState{}, errors.New(responseError(body, response.StatusCode))
	}
	var state receiverState
	err = json.NewDecoder(response.Body).Decode(&state)
	return state, err
}

func initializeRemoteArchive(ctx context.Context, targetURL, token string, size int64, digest, instanceID string) (int64, error) {
	body, _ := json.Marshal(map[string]any{"size": size, "sha256": digest, "instance_id": instanceID})
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, targetURL+"/api/migration/receivers/current/archive", bytes.NewReader(body))
	setReceiverAuthorization(request, token)
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(response.Body)
		return 0, errors.New(responseError(data, response.StatusCode))
	}
	var result struct {
		Offset int64 `json:"offset"`
	}
	err = json.NewDecoder(response.Body).Decode(&result)
	return result.Offset, err
}

func activateRemote(ctx context.Context, targetURL, token string) error {
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, targetURL+"/api/migration/receivers/current/activate", nil)
	setReceiverAuthorization(request, token)
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(response.Body)
		return errors.New(responseError(data, response.StatusCode))
	}
	return nil
}

func waitForReceiverActive(ctx context.Context, targetURL, token string) (receiverState, error) {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return receiverState{}, ctx.Err()
		case <-time.After(time.Second):
		}
		state, err := fetchReceiver(ctx, targetURL, token)
		if err == nil && state.State == "active" {
			return state, nil
		}
		if err == nil && state.State == "error" {
			return receiverState{}, errors.New(state.Error)
		}
	}
	return receiverState{}, errors.New("新服务器启动超时；旧服务器仍保留全部数据")
}

func (a *App) handleCreateHandoff(w http.ResponseWriter, r *http.Request) {
	info, err := a.store.ServerInfo(r.Context())
	if err != nil || info.State != store.ServerStateRetired || info.SuccessorURL == "" {
		writeError(w, http.StatusConflict, "server has not moved")
		return
	}
	deviceID, _ := a.currentDeviceID(r)
	token, err := a.makeHandoffToken(r.Context(), deviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create device handoff")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": info.SuccessorURL + "/#handoff=" + url.QueryEscape(token)})
}

func (a *App) makeHandoffToken(ctx context.Context, deviceID string) (string, error) {
	info, err := a.store.ServerInfo(ctx)
	if err != nil {
		return "", err
	}
	secret, err := a.store.HandoffSecret(ctx)
	if err != nil {
		return "", err
	}
	nonce, _ := randomID()
	payload, _ := json.Marshal(handoffPayload{InstanceID: info.InstanceID, DeviceID: deviceID, Nonce: nonce, ExpiresAt: time.Now().Add(10 * time.Minute).Unix()})
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	key, _ := hex.DecodeString(secret)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (a *App) handleClaimHandoff(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Token string `json:"token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if json.NewDecoder(r.Body).Decode(&request) != nil {
		writeError(w, http.StatusBadRequest, "invalid handoff")
		return
	}
	payload, err := a.verifyHandoffToken(r.Context(), request.Token)
	if err != nil || time.Now().Unix() > payload.ExpiresAt {
		writeError(w, http.StatusUnauthorized, "handoff token is invalid or expired")
		return
	}
	if err := a.store.ClaimHandoffNonce(r.Context(), payload.Nonce, payload.DeviceID); err != nil {
		writeError(w, http.StatusUnauthorized, "handoff token has already been used")
		return
	}
	if err := a.startSession(w, r, payload.DeviceID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not bind device to new server")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) verifyHandoffToken(ctx context.Context, token string) (handoffPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return handoffPayload{}, errors.New("invalid token")
	}
	secret, err := a.store.HandoffSecret(ctx)
	if err != nil {
		return handoffPayload{}, err
	}
	key, _ := hex.DecodeString(secret)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(parts[0]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return handoffPayload{}, errors.New("invalid signature")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return handoffPayload{}, err
	}
	var payload handoffPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return handoffPayload{}, err
	}
	info, err := a.store.ServerInfo(ctx)
	if err != nil || payload.InstanceID != info.InstanceID {
		return handoffPayload{}, errors.New("wrong instance")
	}
	return payload, nil
}

func (a *App) handleClaimReceiver(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Token string `json:"token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if json.NewDecoder(r.Body).Decode(&request) != nil {
		writeError(w, http.StatusBadRequest, "invalid receiver claim")
		return
	}
	state, err := loadReceiver(a.config.DataDir)
	if err != nil || state.State != "active" || !hmac.Equal([]byte(tokenDigest(request.Token)), []byte(state.TokenHash)) || state.HostDeviceID == "" {
		writeError(w, http.StatusUnauthorized, "migration receiver is not ready")
		return
	}
	if err := a.startSession(w, r, state.HostDeviceID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not start migrated server session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func applyPendingMigration(dataDir string, logger *slog.Logger) error {
	state, err := loadReceiver(dataDir)
	if err != nil || state.State != "restarting" {
		return nil
	}
	archivePath := incomingArchivePath(dataDir)
	if state.ExpectedSHA256 != "" {
		if digest, err := hashFile(archivePath); err != nil || digest != state.ExpectedSHA256 {
			state.State = "error"
			state.Error = "迁移包校验失败，旧服务器数据没有被替换"
			_ = saveReceiver(dataDir, state)
			return nil
		}
	}
	staging := filepath.Join(dataDir, "migration", "staging")
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o750); err != nil {
		return err
	}
	manifest, err := extractMigrationArchive(archivePath, staging)
	if err != nil {
		state.State = "error"
		state.Error = err.Error()
		_ = saveReceiver(dataDir, state)
		return nil
	}
	if state.InstanceID != "" && manifest.InstanceID != state.InstanceID {
		state.State = "error"
		state.Error = "迁移实例标识不一致"
		_ = saveReceiver(dataDir, state)
		return nil
	}
	backup := filepath.Join(dataDir, "migration", "previous-"+time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(backup, 0o750); err != nil {
		return err
	}
	for _, name := range []string{"selfsend.db", "selfsend.db-wal", "selfsend.db-shm", "blobs"} {
		oldPath := filepath.Join(dataDir, name)
		if _, err := os.Stat(oldPath); err == nil {
			if err := os.Rename(oldPath, filepath.Join(backup, name)); err != nil {
				return err
			}
		}
	}
	if err := os.Rename(filepath.Join(staging, "selfsend.db"), filepath.Join(dataDir, "selfsend.db")); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(staging, "blobs")); err == nil {
		if err := os.Rename(filepath.Join(staging, "blobs"), filepath.Join(dataDir, "blobs")); err != nil {
			return err
		}
	} else if err := os.MkdirAll(filepath.Join(dataDir, "blobs"), 0o750); err != nil {
		return err
	}
	_ = os.RemoveAll(staging)
	state.State = "applying"
	if err := saveReceiver(dataDir, state); err != nil {
		return err
	}
	logger.Info("migration archive installed", "instance", manifest.InstanceID, "files", len(manifest.Files))
	return nil
}

func finishPendingMigration(dataDir string, db *store.Store, canonicalURL string) error {
	state, err := loadReceiver(dataDir)
	if err != nil || state.State != "applying" {
		return nil
	}
	if canonicalURL == "" {
		canonicalURL = state.BaseURL
	}
	device, err := db.ActivateMigratedServer(context.Background(), state.HostName, state.HostAvatar, canonicalURL)
	if err != nil {
		return err
	}
	if err := db.IntegrityCheck(context.Background()); err != nil {
		return err
	}
	state.State = "active"
	state.BaseURL = canonicalURL
	state.HostDeviceID = device.ID
	state.Error = ""
	return saveReceiver(dataDir, state)
}

func extractMigrationArchive(archivePath, staging string) (archiveManifest, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return archiveManifest{}, err
	}
	defer file.Close()
	reader := tar.NewReader(file)
	actual := make(map[string]archiveManifestFile)
	var manifest archiveManifest
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return archiveManifest{}, err
		}
		name := filepath.Clean(filepath.FromSlash(header.Name))
		if name == "." || filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return archiveManifest{}, errors.New("unsafe path in migration archive")
		}
		if name == "manifest.json" {
			if header.Size > 16<<20 {
				return archiveManifest{}, errors.New("migration manifest is too large")
			}
			if err := json.NewDecoder(io.LimitReader(reader, header.Size)).Decode(&manifest); err != nil {
				return archiveManifest{}, err
			}
			continue
		}
		if name != "selfsend.db" && !strings.HasPrefix(filepath.ToSlash(name), "blobs/") {
			return archiveManifest{}, errors.New("unexpected file in migration archive")
		}
		destination := filepath.Join(staging, name)
		if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
			return archiveManifest{}, err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return archiveManifest{}, err
		}
		hash := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(output, hash), io.LimitReader(reader, header.Size))
		syncErr := output.Sync()
		closeErr := output.Close()
		if copyErr != nil || syncErr != nil || closeErr != nil || written != header.Size {
			return archiveManifest{}, errors.New("could not extract migration archive")
		}
		actual[filepath.ToSlash(name)] = archiveManifestFile{Path: filepath.ToSlash(name), Size: written, SHA256: hex.EncodeToString(hash.Sum(nil))}
	}
	if manifest.Format != 1 || manifest.InstanceID == "" {
		return archiveManifest{}, errors.New("migration manifest is missing")
	}
	for _, expected := range manifest.Files {
		value, ok := actual[expected.Path]
		if !ok || value.Size != expected.Size || value.SHA256 != expected.SHA256 {
			return archiveManifest{}, fmt.Errorf("migration file verification failed: %s", expected.Path)
		}
		delete(actual, expected.Path)
	}
	if len(actual) != 0 {
		return archiveManifest{}, errors.New("migration archive contains unlisted files")
	}
	if _, err := os.Stat(filepath.Join(staging, "selfsend.db")); err != nil {
		return archiveManifest{}, errors.New("migration database is missing")
	}
	return manifest, nil
}

func saveReceiver(dataDir string, state receiverState) error {
	dir := filepath.Join(dataDir, "migration")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	temporary := filepath.Join(dir, "receiver.json.tmp")
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(dir, "receiver.json"))
}

func loadReceiver(dataDir string) (receiverState, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, "migration", "receiver.json"))
	if err != nil {
		return receiverState{}, err
	}
	var state receiverState
	err = json.Unmarshal(data, &state)
	return state, err
}

func incomingArchivePath(dataDir string) string {
	return filepath.Join(dataDir, "migration", "incoming.tar")
}
func tokenDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func migrationCipher(secretHex string) (cipher.AEAD, error) {
	key, err := hex.DecodeString(secretHex)
	if err != nil || len(key) != 32 {
		return nil, errors.New("invalid migration secret")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func migrationNonce(offset int64) []byte {
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[4:], uint64(offset/migrationChunkSize))
	return nonce
}

func migrationAAD(offset int64) []byte { return []byte("selfsend:" + strconv.FormatInt(offset, 10)) }

func encryptMigrationChunk(secretHex string, offset int64, plaintext []byte) ([]byte, error) {
	aead, err := migrationCipher(secretHex)
	if err != nil {
		return nil, err
	}
	return aead.Seal(nil, migrationNonce(offset), plaintext, migrationAAD(offset)), nil
}

func decryptMigrationChunk(secretHex string, offset int64, ciphertext []byte) ([]byte, error) {
	aead, err := migrationCipher(secretHex)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, migrationNonce(offset), ciphertext, migrationAAD(offset))
}

func setReceiverAuthorization(request *http.Request, token string) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	keyDigest := sha256.Sum256([]byte(token))
	mac := hmac.New(sha256.New, keyDigest[:])
	mac.Write([]byte(request.Method + "\n" + request.URL.EscapedPath() + "\n" + timestamp))
	request.Header.Set("X-SelfSend-Time", timestamp)
	request.Header.Set("Authorization", "SelfSend "+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
}

func validReceiverAuthorization(request *http.Request, secretHex string) bool {
	authorization := request.Header.Get("Authorization")
	if strings.HasPrefix(authorization, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
		return token != "" && hmac.Equal([]byte(tokenDigest(token)), []byte(secretHex))
	}
	if !strings.HasPrefix(authorization, "SelfSend ") {
		return false
	}
	timestamp := request.Header.Get("X-SelfSend-Time")
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || seconds < time.Now().Add(-90*time.Second).Unix() || seconds > time.Now().Add(90*time.Second).Unix() {
		return false
	}
	key, err := hex.DecodeString(secretHex)
	if err != nil || len(key) != 32 {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(request.Method + "\n" + request.URL.EscapedPath() + "\n" + timestamp))
	provided, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(authorization, "SelfSend ")))
	return err == nil && hmac.Equal(provided, mac.Sum(nil))
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func validateMigrationTarget(value, mode string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return "", errors.New("invalid target")
	}
	if mode == "online" && parsed.Scheme != "https" {
		return "", errors.New("online target must use HTTPS")
	}
	host := strings.ToLower(parsed.Hostname())
	if mode != "online" && (host == "localhost" || strings.HasSuffix(host, ".local")) {
		return strings.TrimRight(parsed.Scheme+"://"+parsed.Host, "/"), nil
	}
	addresses, err := net.LookupIP(host)
	if err != nil || len(addresses) == 0 {
		return "", errors.New("could not resolve target")
	}
	if mode == "online" {
		for _, address := range addresses {
			if address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsUnspecified() || address.IsMulticast() {
				return "", errors.New("online target must resolve to public addresses")
			}
		}
	} else {
		for _, address := range addresses {
			if !address.IsPrivate() && !address.IsLoopback() && !address.IsLinkLocalUnicast() {
				return "", errors.New("target must be on the local network")
			}
		}
	}
	return strings.TrimRight(parsed.Scheme+"://"+parsed.Host, "/"), nil
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func availableDisk(path string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

func responseError(body []byte, status int) string {
	var value struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &value) == nil && value.Error != "" {
		return value.Error
	}
	return fmt.Sprintf("HTTP %d", status)
}
