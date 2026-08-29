package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	ServerStateActive    = "active"
	ServerStateMigrating = "migrating"
	ServerStateRetired   = "retired"
)

type ServerInfo struct {
	InstanceID       string `json:"instance_id"`
	InstanceName     string `json:"instance_name"`
	CanonicalURL     string `json:"canonical_url"`
	State            string `json:"state"`
	SuccessorURL     string `json:"successor_url,omitempty"`
	ServerDeviceID   string `json:"server_device_id,omitempty"`
	ServerDeviceName string `json:"server_device_name,omitempty"`
	MigrationEpoch   int64  `json:"migration_epoch"`
}

func (s *Store) ensureServerSettings(ctx context.Context) error {
	instanceID, err := randomSettingValue(16)
	if err != nil {
		return err
	}
	handoffSecret, err := randomSettingValue(32)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO settings(key, value) VALUES
  ('instance_id', ?),
  ('instance_name', 'SelfSend'),
  ('canonical_url', ''),
  ('server_state', 'active'),
  ('successor_url', ''),
  ('migration_epoch', '0'),
  ('handoff_secret', ?);`, instanceID, handoffSecret)
	return err
}

func randomSettingValue(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return value, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) ServerInfo(ctx context.Context) (ServerInfo, error) {
	keys := []string{"instance_id", "instance_name", "canonical_url", "server_state", "successor_url", "server_device_id", "migration_epoch"}
	values := make(map[string]string, len(keys))
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings WHERE key IN ('instance_id','instance_name','canonical_url','server_state','successor_url','server_device_id','migration_epoch')`)
	if err != nil {
		return ServerInfo{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return ServerInfo{}, err
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return ServerInfo{}, err
	}
	epoch, _ := strconv.ParseInt(values["migration_epoch"], 10, 64)
	info := ServerInfo{
		InstanceID: values["instance_id"], InstanceName: values["instance_name"], CanonicalURL: values["canonical_url"],
		State: values["server_state"], SuccessorURL: values["successor_url"], ServerDeviceID: values["server_device_id"], MigrationEpoch: epoch,
	}
	if info.State == "" {
		info.State = ServerStateActive
	}
	if info.ServerDeviceID != "" {
		_ = s.db.QueryRowContext(ctx, `SELECT name FROM devices WHERE id = ?`, info.ServerDeviceID).Scan(&info.ServerDeviceName)
	}
	return info, nil
}

func (s *Store) SetServerState(ctx context.Context, state, successorURL string) error {
	if state != ServerStateActive && state != ServerStateMigrating && state != ServerStateRetired {
		return fmt.Errorf("invalid server state %q", state)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range map[string]string{"server_state": state, "successor_url": strings.TrimRight(successorURL, "/")} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ActivateMigratedServer(ctx context.Context, hostName, hostAvatar, canonicalURL string) (Device, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Device{}, err
	}
	defer tx.Rollback()

	hostName = strings.TrimSpace(hostName)
	if hostName == "" {
		hostName = "新服务器"
	}
	if hostAvatar == "" {
		hostAvatar = "💻"
	}
	var deviceID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM devices WHERE name = ? COLLATE NOCASE LIMIT 1`, hostName).Scan(&deviceID)
	if errors.Is(err, sql.ErrNoRows) {
		deviceID, err = randomSettingValue(16)
		if err != nil {
			return Device{}, err
		}
		hostName, err = availableDeviceName(ctx, tx, hostName, "")
		if err != nil {
			return Device{}, err
		}
		now := time.Now().UnixMilli()
		if _, err := tx.ExecContext(ctx, `INSERT INTO devices(id, name, avatar, created_at, last_seen_at) VALUES(?, ?, ?, ?, ?)`, deviceID, hostName, hostAvatar, now, now); err != nil {
			return Device{}, err
		}
		if err := connectDeviceToAll(ctx, tx, deviceID, now); err != nil {
			return Device{}, err
		}
	} else if err != nil {
		return Device{}, err
	}
	var epoch int64
	_ = tx.QueryRowContext(ctx, `SELECT CAST(value AS INTEGER) FROM settings WHERE key = 'migration_epoch'`).Scan(&epoch)
	settings := map[string]string{
		"server_device_id": deviceID,
		"server_state":     ServerStateActive,
		"successor_url":    "",
		"canonical_url":    strings.TrimRight(canonicalURL, "/"),
		"migration_epoch":  strconv.FormatInt(epoch+1, 10),
	}
	for key, value := range settings {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			return Device{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Device{}, err
	}
	return s.Device(ctx, deviceID)
}

func (s *Store) PendingUploadCount(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM uploads`).Scan(&count)
	return count, err
}

func (s *Store) Checkpoint(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

func (s *Store) IntegrityCheck(ctx context.Context) error {
	var result string
	if err := s.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("sqlite integrity check: %s", result)
	}
	return nil
}

func (s *Store) HandoffSecret(ctx context.Context) (string, error) {
	return s.Setting(ctx, "handoff_secret")
}

func (s *Store) ClaimHandoffNonce(ctx context.Context, nonce, deviceID string) error {
	result, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO handoff_claims(nonce, device_id, claimed_at) SELECT ?, id, ? FROM devices WHERE id = ?`, nonce, time.Now().UnixMilli(), deviceID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrNotFound
	}
	return nil
}
