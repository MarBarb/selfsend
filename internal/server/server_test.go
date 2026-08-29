package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestSetupUploadTimelineDownloadAndDelete(t *testing.T) {
	app, httpServer, client := newTestServer(t)
	defer app.Close()
	defer httpServer.Close()

	status := getStatus(t, client, httpServer.URL)
	if !status.SetupRequired || status.Authenticated {
		t.Fatalf("unexpected initial status: %+v", status)
	}

	response := jsonRequest(t, client, http.MethodPost, httpServer.URL+"/api/setup", `{"password":"a-long-test-password"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()

	status = getStatus(t, client, httpServer.URL)
	if status.SetupRequired || !status.Authenticated {
		t.Fatalf("unexpected initialized status: %+v", status)
	}
	response = jsonRequest(t, client, http.MethodPost, httpServer.URL+"/api/devices/register", `{"id":"device-test-1234567890","name":"测试电脑","avatar":"💻"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("register device status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	_, conversationID := pairDevice(t, httpServer, client, "测试手机", "📱")

	content := []byte("hello from SelfSend")
	metadata := "filename " + base64.StdEncoding.EncodeToString([]byte("../hello.txt")) +
		",filetype " + base64.StdEncoding.EncodeToString([]byte("text/plain")) +
		",conversation " + base64.StdEncoding.EncodeToString([]byte(conversationID))
	request, err := http.NewRequest(http.MethodPost, httpServer.URL+"/api/uploads", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Tus-Resumable", tusVersion)
	request.Header.Set("Upload-Length", "19")
	request.Header.Set("Upload-Metadata", metadata)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create upload status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	location := response.Header.Get("Location")
	response.Body.Close()
	if !strings.HasPrefix(location, "/api/uploads/") {
		t.Fatalf("invalid upload location %q", location)
	}

	patchUpload(t, client, httpServer.URL+location, 0, content[:5], 5)
	patchUpload(t, client, httpServer.URL+location, 5, content[5:], int64(len(content)))

	response, err = client.Get(httpServer.URL + "/api/items?conversation_id=" + conversationID)
	if err != nil {
		t.Fatal(err)
	}
	var timeline struct {
		Items []struct {
			ID       string `json:"id"`
			Kind     string `json:"kind"`
			FileName string `json:"file_name"`
			Size     int64  `json:"size"`
			SHA256   string `json:"sha256"`
		} `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&timeline); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(timeline.Items) != 1 {
		t.Fatalf("timeline has %d items", len(timeline.Items))
	}
	item := timeline.Items[0]
	if item.Kind != "file" || item.FileName != "hello.txt" || item.Size != int64(len(content)) {
		t.Fatalf("unexpected item: %+v", item)
	}
	digest := sha256.Sum256(content)
	if item.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("unexpected sha256 %q", item.SHA256)
	}

	request, _ = http.NewRequest(http.MethodGet, httpServer.URL+"/api/files/"+item.ID, nil)
	request.Header.Set("Range", "bytes=0-4")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusPartialContent || readBody(t, response) != "hello" {
		t.Fatalf("unexpected range response status %d", response.StatusCode)
	}
	response.Body.Close()

	response = jsonRequest(t, client, http.MethodPost, httpServer.URL+"/api/notes", `{"conversation_id":"`+conversationID+`","text":"  记得在另一台设备上打开  "}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create note status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var note struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(response.Body).Decode(&note); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if note.Kind != "text" || note.Text != "记得在另一台设备上打开" {
		t.Fatalf("unexpected note: %+v", note)
	}

	response, err = client.Get(httpServer.URL + "/api/items?conversation_id=" + conversationID)
	if err != nil {
		t.Fatal(err)
	}
	var mixedTimeline struct {
		Items []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
			Text string `json:"text"`
		} `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&mixedTimeline); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(mixedTimeline.Items) != 2 || mixedTimeline.Items[0].Kind != "text" || mixedTimeline.Items[0].Text != note.Text {
		t.Fatalf("unexpected mixed timeline: %+v", mixedTimeline.Items)
	}

	request, _ = http.NewRequest(http.MethodDelete, httpServer.URL+"/api/items/"+note.ID, nil)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete note status = %d", response.StatusCode)
	}
	response.Body.Close()

	request, _ = http.NewRequest(http.MethodDelete, httpServer.URL+"/api/items/"+item.ID, nil)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", response.StatusCode)
	}
	response.Body.Close()

	status = getStatus(t, client, httpServer.URL)
	if status.ItemCount != 0 || status.TotalBytes != 0 {
		t.Fatalf("unexpected final stats: %+v", status)
	}
}

func TestAuthenticationAndOriginProtection(t *testing.T) {
	app, httpServer, client := newTestServer(t)
	defer app.Close()
	defer httpServer.Close()

	response := jsonRequest(t, client, http.MethodPost, httpServer.URL+"/api/setup", `{"password":"a-long-test-password"}`)
	response.Body.Close()
	response = jsonRequest(t, client, http.MethodPost, httpServer.URL+"/api/logout", "")
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d", response.StatusCode)
	}
	response.Body.Close()

	response = jsonRequest(t, client, http.MethodPost, httpServer.URL+"/api/login", `{"password":"definitely-wrong"}`)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d", response.StatusCode)
	}
	response.Body.Close()

	request, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/api/login", strings.NewReader(`{"password":"a-long-test-password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.example")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", response.StatusCode)
	}
	response.Body.Close()

	response = jsonRequest(t, client, http.MethodPost, httpServer.URL+"/api/login", `{"password":"a-long-test-password"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
}

func TestWebRootDoesNotRedirect(t *testing.T) {
	app, httpServer, client := newTestServer(t)
	defer app.Close()
	defer httpServer.Close()

	response, err := client.Get(httpServer.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("root status = %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("root content type = %q", contentType)
	}
}

type statusResponse struct {
	SetupRequired bool  `json:"setup_required"`
	Authenticated bool  `json:"authenticated"`
	ItemCount     int64 `json:"item_count"`
	TotalBytes    int64 `json:"total_bytes"`
}

func newTestServer(t *testing.T) (*App, *httptest.Server, *http.Client) {
	t.Helper()
	app, err := New(Config{DataDir: t.TempDir(), MaxUploadSize: 1 << 20, Version: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler())
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Jar = jar
	return app, server, client
}

func newTestClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: server.Client().Transport, Jar: jar}
}

func registerDevice(t *testing.T, client *http.Client, baseURL, id, name, avatar string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"id": id, "name": name, "avatar": avatar})
	response := jsonRequest(t, client, http.MethodPost, baseURL+"/api/devices/register", string(body))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("register device status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
}

func pairDevice(t *testing.T, server *httptest.Server, inviter *http.Client, name, avatar string) (*http.Client, string) {
	t.Helper()
	response := jsonRequest(t, inviter, http.MethodPost, server.URL+"/api/pairing/invites", `{}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create invite status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var invite struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&invite); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	device := newTestClient(t, server)
	body, _ := json.Marshal(map[string]string{"token": invite.Token, "name": name, "avatar": avatar})
	response = jsonRequest(t, device, http.MethodPost, server.URL+"/api/pairing/claim", string(body))
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("claim invite status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var claim struct {
		Device struct {
			ID string `json:"id"`
		} `json:"device"`
	}
	if err := json.NewDecoder(response.Body).Decode(&claim); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	return device, conversationWith(t, inviter, server.URL, claim.Device.ID)
}

func conversationWith(t *testing.T, client *http.Client, baseURL, deviceID string) string {
	t.Helper()
	response, err := client.Get(baseURL + "/api/conversations")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var conversations struct {
		Conversations []struct {
			ID             string `json:"id"`
			ConversationID string `json:"conversation_id"`
		} `json:"conversations"`
	}
	if err := json.NewDecoder(response.Body).Decode(&conversations); err != nil {
		t.Fatal(err)
	}
	for _, device := range conversations.Conversations {
		if device.ID == deviceID {
			return device.ConversationID
		}
	}
	t.Fatalf("conversation with %s not found: %+v", deviceID, conversations.Conversations)
	return ""
}

func getStatus(t *testing.T, client *http.Client, baseURL string) statusResponse {
	t.Helper()
	response, err := client.Get(baseURL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var status statusResponse
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func jsonRequest(t *testing.T, client *http.Client, method, requestURL, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, requestURL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func patchUpload(t *testing.T, client *http.Client, uploadURL string, offset int64, body []byte, expectedOffset int64) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPatch, uploadURL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/offset+octet-stream")
	request.Header.Set("Tus-Resumable", tusVersion)
	request.Header.Set("Upload-Offset", fmtInt(offset))
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("patch status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	if response.Header.Get("Upload-Offset") != fmtInt(expectedOffset) {
		t.Fatalf("patch offset = %q", response.Header.Get("Upload-Offset"))
	}
}

func fmtInt(value int64) string { return strconv.FormatInt(value, 10) }

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestSameOrigin(t *testing.T) {
	parsed, _ := url.Parse("https://selfsend.example")
	if !sameOrigin(parsed.String(), parsed.Host) || sameOrigin("https://evil.example", parsed.Host) {
		t.Fatal("sameOrigin returned an unexpected result")
	}
}
