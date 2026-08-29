package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestPairingCreatesFriendConversationAndDirectionalMessages(t *testing.T) {
	app, httpServer, mac := newTestServer(t)
	defer app.Close()
	defer httpServer.Close()

	response := jsonRequest(t, mac, http.MethodPost, httpServer.URL+"/api/setup", `{"password":"a-long-test-password"}`)
	response.Body.Close()
	registerDevice(t, mac, httpServer.URL, "device-mac-123456789", "Mac", "💻")

	response = jsonRequest(t, mac, http.MethodPost, httpServer.URL+"/api/pairing/invites", `{}`)
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

	phone := newTestClient(t, httpServer)
	response = jsonRequest(t, phone, http.MethodPost, httpServer.URL+"/api/pairing/claim", `{"token":"`+invite.Token+`","name":"iPhone","avatar":"📱"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("claim invite status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var claim struct {
		Device struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"device"`
	}
	if err := json.NewDecoder(response.Body).Decode(&claim); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	conversationID := conversationWith(t, mac, httpServer.URL, claim.Device.ID)
	if phoneConversation := conversationWith(t, phone, httpServer.URL, "device-mac-123456789"); phoneConversation != conversationID {
		t.Fatalf("conversation mismatch: %q != %q", phoneConversation, conversationID)
	}
	response = jsonRequest(t, mac, http.MethodPost, httpServer.URL+"/api/pairing/invites", `{}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create second invite status = %d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(&invite); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	secondPhone := newTestClient(t, httpServer)
	response = jsonRequest(t, secondPhone, http.MethodPost, httpServer.URL+"/api/pairing/claim", `{"token":"`+invite.Token+`","name":"iPhone","avatar":"📱"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("claim second device status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var secondClaim struct {
		Device struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"device"`
	}
	if err := json.NewDecoder(response.Body).Decode(&secondClaim); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if secondClaim.Device.Name != "iPhone(2)" {
		t.Fatalf("duplicate device name = %q", secondClaim.Device.Name)
	}
	conversationWith(t, mac, httpServer.URL, secondClaim.Device.ID)
	conversationWith(t, phone, httpServer.URL, secondClaim.Device.ID)
	conversationWith(t, secondPhone, httpServer.URL, claim.Device.ID)
	conversationWith(t, secondPhone, httpServer.URL, "device-mac-123456789")
	response = jsonRequest(t, mac, http.MethodPost, httpServer.URL+"/api/groups", `{"device_ids":["`+claim.Device.ID+`","`+secondClaim.Device.ID+`"]}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create group status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var group struct {
		ID             string `json:"id"`
		ConversationID string `json:"conversation_id"`
		Kind           string `json:"kind"`
		MemberCount    int    `json:"member_count"`
	}
	if err := json.NewDecoder(response.Body).Decode(&group); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if group.Kind != "group" || group.MemberCount != 3 {
		t.Fatalf("unexpected group: %+v", group)
	}
	for _, client := range []*http.Client{mac, phone, secondPhone} {
		if got := conversationWith(t, client, httpServer.URL, group.ID); got != group.ConversationID {
			t.Fatalf("group conversation = %q, want %q", got, group.ConversationID)
		}
	}
	response = jsonRequest(t, phone, http.MethodPost, httpServer.URL+"/api/notes", `{"conversation_id":"`+group.ConversationID+`","text":"群里收到"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("group reply status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	response, _ = secondPhone.Get(httpServer.URL + "/api/items?conversation_id=" + group.ConversationID)
	var groupTimeline struct {
		Items []struct {
			Text       string `json:"text"`
			SenderName string `json:"sender_name"`
		} `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&groupTimeline); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(groupTimeline.Items) != 1 || groupTimeline.Items[0].Text != "群里收到" || groupTimeline.Items[0].SenderName != "iPhone" {
		t.Fatalf("unexpected group timeline: %+v", groupTimeline.Items)
	}

	response = jsonRequest(t, mac, http.MethodPost, httpServer.URL+"/api/notes", `{"conversation_id":"`+conversationID+`","text":"发给手机"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create note status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var sent struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&sent); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	response, _ = phone.Get(httpServer.URL + "/api/items?conversation_id=" + conversationID)
	var timeline struct {
		Items []struct {
			Text   string `json:"text"`
			Sender string `json:"sender_device_id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&timeline); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(timeline.Items) != 1 || timeline.Items[0].Text != "发给手机" || timeline.Items[0].Sender != "device-mac-123456789" {
		t.Fatalf("unexpected timeline: %+v", timeline.Items)
	}
	response = jsonRequest(t, phone, http.MethodDelete, httpServer.URL+"/api/items/"+sent.ID, "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("delete another device's message status = %d", response.StatusCode)
	}
	response.Body.Close()
	response = jsonRequest(t, phone, http.MethodPost, httpServer.URL+"/api/notes", `{"conversation_id":"`+conversationID+`","text":"手机收到"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("reply status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	response, _ = mac.Get(httpServer.URL + "/api/items?conversation_id=" + conversationID)
	if err := json.NewDecoder(response.Body).Decode(&timeline); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(timeline.Items) != 2 || timeline.Items[0].Text != "手机收到" || timeline.Items[0].Sender != claim.Device.ID {
		t.Fatalf("unexpected reply timeline: %+v", timeline.Items)
	}

	response = jsonRequest(t, phone, http.MethodPatch, httpServer.URL+"/api/devices/"+claim.Device.ID, `{"name":"随身手机","avatar":"🐼"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("update device status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()

	response = jsonRequest(t, phone, http.MethodPost, httpServer.URL+"/api/pairing/claim", `{"token":"`+invite.Token+`","name":"iPhone","avatar":"📱"}`)
	if response.StatusCode != http.StatusGone {
		t.Fatalf("reused invitation status = %d", response.StatusCode)
	}
	response.Body.Close()
}
