package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestServerDeploymentSettings(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	info, err := db.ServerInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.DeploymentType != DeploymentLocal || info.Provider != "computer" {
		t.Fatalf("default deployment = %q/%q", info.DeploymentType, info.Provider)
	}
	if err := db.SetDeployment(ctx, DeploymentNAS, "zspace"); err != nil {
		t.Fatal(err)
	}
	info, err = db.ServerInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.DeploymentType != DeploymentNAS || info.Provider != "zspace" {
		t.Fatalf("saved deployment = %q/%q", info.DeploymentType, info.Provider)
	}
	if err := db.SetDeployment(ctx, "unsupported", ""); err == nil {
		t.Fatal("unsupported deployment type was accepted")
	}
}

func TestDeviceNamesAreUniqueAndAllDevicesAreConnected(t *testing.T) {
	dataDir := t.TempDir()
	store, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mac, err := store.RegisterDevice(ctx, "device-mac-123456", "Mac", "💻")
	if err != nil {
		t.Fatal(err)
	}
	phone, err := store.RegisterDevice(ctx, "device-phone-123456", "iPhone", "📱")
	if err != nil {
		t.Fatal(err)
	}
	secondPhone, err := store.RegisterDevice(ctx, "device-phone-234567", "iPhone", "📱")
	if err != nil {
		t.Fatal(err)
	}
	if phone.Name != "iPhone" || secondPhone.Name != "iPhone(2)" {
		t.Fatalf("unexpected names: %q, %q", phone.Name, secondPhone.Name)
	}
	if !mac.IsServer || phone.IsServer || secondPhone.IsServer {
		t.Fatalf("unexpected server flags: mac=%v phone=%v second=%v", mac.IsServer, phone.IsServer, secondPhone.IsServer)
	}
	for _, device := range []Device{mac, phone, secondPhone} {
		conversations, err := store.ListConversations(ctx, device.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(conversations) != 2 {
			t.Fatalf("%s has %d conversations", device.Name, len(conversations))
		}
	}
	store.Close()

	// Simulate a database created before unique device names were introduced.
	legacy, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.db.Exec(`DROP INDEX idx_devices_name_unique`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	if _, err := legacy.db.Exec(`INSERT INTO devices(id, name, avatar, created_at, last_seen_at) VALUES(?, 'iPhone', '📱', ?, ?)`, "device-phone-345678", now, now); err != nil {
		t.Fatal(err)
	}
	legacy.Close()

	migrated, err := Open(filepath.Clean(dataDir))
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	device, err := migrated.Device(ctx, "device-phone-345678")
	if err != nil {
		t.Fatal(err)
	}
	if device.Name != "iPhone(3)" {
		t.Fatalf("migrated duplicate name = %q", device.Name)
	}
	conversations, err := migrated.ListConversations(ctx, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 3 {
		t.Fatalf("migrated device has %d conversations", len(conversations))
	}
}

func TestGroupMembershipControlsConversationAccess(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	devices := make([]Device, 0, 4)
	for index, name := range []string{"Mac", "iPhone", "iPad", "Windows"} {
		device, err := store.RegisterDevice(ctx, fmt.Sprintf("device-group-%d-123456", index), name, "设备")
		if err != nil {
			t.Fatal(err)
		}
		devices = append(devices, device)
	}
	group, err := store.CreateGroup(ctx, "group-test-123456", "conversation-group-123456", devices[0].ID, []string{devices[1].ID, devices[2].ID})
	if err != nil {
		t.Fatal(err)
	}
	if group.MemberCount != 3 {
		t.Fatalf("member count = %d", group.MemberCount)
	}
	for index, device := range devices {
		member, err := store.ConversationMember(ctx, group.ConversationID, device.ID)
		if err != nil {
			t.Fatal(err)
		}
		if member != (index < 3) {
			t.Fatalf("device %s membership = %v", device.Name, member)
		}
	}
}
