package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/MarBarb/selfsend/internal/store"
)

func TestMigrationArchiveReceiverActivationAndClaim(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	oldDir := t.TempDir()
	oldApp, err := New(Config{DataDir: oldDir, Version: "test"}, logger)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	passwordHash, err := hashPassword("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := oldApp.store.InitializePassword(ctx, passwordHash); err != nil {
		t.Fatal(err)
	}
	mac, err := oldApp.store.RegisterDevice(ctx, "migration-mac-123456", "Mac", "💻")
	if err != nil {
		t.Fatal(err)
	}
	phone, err := oldApp.store.RegisterDevice(ctx, "migration-phone-123456", "iPhone", "📱")
	if err != nil {
		t.Fatal(err)
	}
	conversationID := directConversationID(t, oldApp.store, mac.ID, phone.ID)
	if _, err := oldApp.store.CreateNote(ctx, "migration-note-123456", conversationID, mac.ID, "迁移后仍然存在"); err != nil {
		t.Fatal(err)
	}
	blobContent := []byte("portable migration file")
	blobID := "migration-file-123456"
	blobDir := filepath.Join(oldDir, "blobs", blobID)
	if err := os.MkdirAll(blobDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blobDir, "data"), blobContent, 0o600); err != nil {
		t.Fatal(err)
	}
	temporary := filepath.Join(oldDir, "uploads", blobID+".part")
	if err := os.WriteFile(temporary, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	upload := store.Upload{ID: blobID, ConversationID: conversationID, SenderDeviceID: mac.ID, FileName: "portable.txt", MimeType: "text/plain", TotalSize: int64(len(blobContent)), TempPath: temporary}
	if err := oldApp.store.CreateUpload(ctx, upload); err != nil {
		t.Fatal(err)
	}
	digestBytes := sha256.Sum256(blobContent)
	if err := oldApp.store.CompleteUpload(ctx, upload, hex.EncodeToString(digestBytes[:]), filepath.Join("blobs", blobID, "data")); err != nil {
		t.Fatal(err)
	}
	if err := oldApp.store.SetServerState(ctx, store.ServerStateMigrating, "http://new-server.local:8080"); err != nil {
		t.Fatal(err)
	}
	archivePath, manifest, digest, err := oldApp.createMigrationArchive(ctx, "migration-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(archivePath)
	oldInfo, err := oldApp.store.ServerInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oldApp.Close()

	targetDir := t.TempDir()
	targetApp, err := New(Config{DataDir: targetDir, Version: "test"}, logger)
	if err != nil {
		t.Fatal(err)
	}
	targetServer := httptest.NewServer(targetApp.Handler())
	targetClient := targetServer.Client()

	response := jsonRequest(t, targetClient, http.MethodPost, targetServer.URL+"/api/migration/receivers", `{"name":"Windows","avatar":"🖥️"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create receiver: %d %s", response.StatusCode, readBody(t, response))
	}
	var receiver struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&receiver); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if state, err := fetchReceiver(context.Background(), targetServer.URL, receiver.Token); err != nil || state.HostName != "Windows" {
		t.Fatalf("signed receiver discovery = %+v, err = %v", state, err)
	}

	archiveStat, _ := os.Stat(archivePath)
	initBody, _ := json.Marshal(map[string]any{"size": archiveStat.Size(), "sha256": digest, "instance_id": manifest.InstanceID})
	request, _ := http.NewRequest(http.MethodPost, targetServer.URL+"/api/migration/receivers/current/archive", bytes.NewReader(initBody))
	request.Header.Set("Authorization", "Bearer "+receiver.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err = targetClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("initialize receiver: %d %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()

	archive, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	offset := int64(0)
	buffer := make([]byte, 64<<10)
	for {
		count, readErr := archive.Read(buffer)
		if count > 0 {
			ciphertext, err := encryptMigrationChunk(tokenDigest(receiver.Token), offset, buffer[:count])
			if err != nil {
				t.Fatal(err)
			}
			request, _ = http.NewRequest(http.MethodPatch, targetServer.URL+"/api/migration/receivers/current/archive", bytes.NewReader(ciphertext))
			request.Header.Set("Authorization", "Bearer "+receiver.Token)
			request.Header.Set("Content-Type", "application/offset+octet-stream")
			request.Header.Set("Upload-Offset", strconv.FormatInt(offset, 10))
			request.Header.Set("X-SelfSend-Migration-Encrypted", "1")
			request.Header.Set("X-SelfSend-Plain-Length", strconv.Itoa(count))
			response, err = targetClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusNoContent {
				t.Fatalf("upload archive: %d %s", response.StatusCode, readBody(t, response))
			}
			response.Body.Close()
			offset += int64(count)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
	}
	request, _ = http.NewRequest(http.MethodPost, targetServer.URL+"/api/migration/receivers/current/activate", nil)
	request.Header.Set("Authorization", "Bearer "+receiver.Token)
	response, err = targetClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("activate receiver: %d %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	targetServer.Close()
	targetApp.Close()

	migratedApp, err := New(Config{DataDir: targetDir, Version: "test", CanonicalURL: "http://new-server.local:8080"}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer migratedApp.Close()
	migratedServer := httptest.NewServer(migratedApp.Handler())
	defer migratedServer.Close()
	jar, _ := cookiejar.New(nil)
	migratedClient := migratedServer.Client()
	migratedClient.Jar = jar
	response = jsonRequest(t, migratedClient, http.MethodPost, migratedServer.URL+"/api/migration/receiver/claim", `{"token":"`+receiver.Token+`"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("claim receiver: %d %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()

	status := getStatus(t, migratedClient, migratedServer.URL)
	if status.SetupRequired || !status.Authenticated {
		t.Fatalf("unexpected migrated status: %+v", status)
	}
	newInfo, err := migratedApp.store.ServerInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if newInfo.InstanceID != oldInfo.InstanceID || newInfo.State != store.ServerStateActive || newInfo.ServerDeviceName != "Windows" {
		t.Fatalf("unexpected migrated server info: %+v", newInfo)
	}
	items, err := migratedApp.store.ListTimeline(ctx, conversationID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].FileName != "portable.txt" || items[1].Text != "迁移后仍然存在" {
		t.Fatalf("unexpected migrated timeline: %+v", items)
	}
	if data, err := os.ReadFile(filepath.Join(targetDir, "blobs", blobID, "data")); err != nil || !bytes.Equal(data, blobContent) {
		t.Fatalf("migrated blob = %q, err = %v", data, err)
	}
}

func TestValidateMigrationTarget(t *testing.T) {
	tests := []struct {
		name  string
		value string
		mode  string
		want  string
		ok    bool
	}{
		{name: "local IPv4", value: "http://192.168.1.20:8080/path", mode: "local", want: "http://192.168.1.20:8080", ok: true},
		{name: "local hostname", value: "http://selfsend.local:8080", mode: "local", want: "http://selfsend.local:8080", ok: true},
		{name: "public HTTPS", value: "https://1.1.1.1/receiver", mode: "online", want: "https://1.1.1.1", ok: true},
		{name: "public HTTP rejected", value: "http://1.1.1.1", mode: "online", ok: false},
		{name: "private online rejected", value: "https://192.168.1.20", mode: "online", ok: false},
		{name: "public local rejected", value: "https://1.1.1.1", mode: "local", ok: false},
		{name: "credentials rejected", value: "https://user:pass@1.1.1.1", mode: "online", ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateMigrationTarget(test.value, test.mode)
			if test.ok && err != nil {
				t.Fatalf("validateMigrationTarget() error = %v", err)
			}
			if !test.ok && err == nil {
				t.Fatalf("validateMigrationTarget() = %q, want error", got)
			}
			if got != test.want {
				t.Fatalf("validateMigrationTarget() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAnyRegisteredDeviceCanPrepareAndDownloadBackup(t *testing.T) {
	app, server, client := newTestServer(t)
	defer app.Close()
	defer server.Close()
	response := jsonRequest(t, client, http.MethodPost, server.URL+"/api/setup", `{"password":"a-long-test-password"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	response = jsonRequest(t, client, http.MethodPost, server.URL+"/api/devices/register", `{"id":"backup-mac-123456789","name":"Mac","avatar":"💻"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("register: %d %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()

	ordinaryDevice := newTestClient(t, server)
	response = jsonRequest(t, ordinaryDevice, http.MethodPost, server.URL+"/api/login", `{"password":"a-long-test-password"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login ordinary device: %d %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	response = jsonRequest(t, ordinaryDevice, http.MethodPost, server.URL+"/api/devices/register", `{"id":"backup-phone-1234567","name":"iPhone","avatar":"📱"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("register ordinary device: %d %s", response.StatusCode, readBody(t, response))
	}
	var registered store.Device
	if err := json.NewDecoder(response.Body).Decode(&registered); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if registered.IsServer {
		t.Fatal("ordinary device unexpectedly became the server device")
	}

	response = jsonRequest(t, ordinaryDevice, http.MethodPost, server.URL+"/api/server/backups", `{"password":"wrong-password"}`)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password backup status: %d %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	response = jsonRequest(t, ordinaryDevice, http.MethodPost, server.URL+"/api/server/backups", `{"password":"a-long-test-password"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("prepare backup: %d %s", response.StatusCode, readBody(t, response))
	}
	var prepared struct {
		DownloadURL string `json:"download_url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&prepared); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	response, err := ordinaryDevice.Get(server.URL + prepared.DownloadURL)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/x-tar" {
		t.Fatalf("download backup: %d, type %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || len(data) < 1024 {
		t.Fatalf("backup size = %d, err = %v", len(data), err)
	}
}

func directConversationID(t *testing.T, database *store.Store, first, second string) string {
	t.Helper()
	conversations, err := database.ListConversations(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	for _, conversation := range conversations {
		if conversation.ID == second {
			return conversation.ConversationID
		}
	}
	t.Fatal("direct conversation not found")
	return ""
}
