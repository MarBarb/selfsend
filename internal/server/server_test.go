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

	content := []byte("hello from SelfSend")
	metadata := "filename " + base64.StdEncoding.EncodeToString([]byte("../hello.txt")) +
		",filetype " + base64.StdEncoding.EncodeToString([]byte("text/plain"))
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

	response, err = client.Get(httpServer.URL + "/api/items")
	if err != nil {
		t.Fatal(err)
	}
	var timeline struct {
		Items []struct {
			ID       string `json:"id"`
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
	if item.FileName != "hello.txt" || item.Size != int64(len(content)) {
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
