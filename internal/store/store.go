package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db             *sql.DB
	timelineMu     sync.Mutex
	lastTimelineAt int64
}

type Upload struct {
	ID             string
	ConversationID string
	SenderDeviceID string
	FileName       string
	MimeType       string
	TotalSize      int64
	Offset         int64
	TempPath       string
	LastModified   int64
}

type Item struct {
	ID             string `json:"id"`
	ConversationID string `json:"-"`
	SenderDeviceID string `json:"sender_device_id"`
	FileName       string `json:"file_name"`
	MimeType       string `json:"mime_type"`
	Size           int64  `json:"size"`
	SHA256         string `json:"sha256"`
	CreatedAt      int64  `json:"created_at"`
	LastModified   int64  `json:"last_modified,omitempty"`
	StoragePath    string `json:"-"`
}

type TimelineItem struct {
	Kind           string `json:"kind"`
	ID             string `json:"id"`
	Text           string `json:"text,omitempty"`
	FileName       string `json:"file_name,omitempty"`
	MimeType       string `json:"mime_type,omitempty"`
	Size           int64  `json:"size,omitempty"`
	SHA256         string `json:"sha256,omitempty"`
	CreatedAt      int64  `json:"created_at"`
	SenderDeviceID string `json:"sender_device_id"`
	SenderName     string `json:"sender_name"`
	SenderAvatar   string `json:"sender_avatar"`
	LastModified   int64  `json:"last_modified,omitempty"`
}

type Device struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Avatar         string `json:"avatar"`
	CreatedAt      int64  `json:"created_at"`
	LastSeenAt     int64  `json:"last_seen_at"`
	LastMessageAt  int64  `json:"last_message_at"`
	LastKind       string `json:"last_kind,omitempty"`
	LastPreview    string `json:"last_preview,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	IsServer       bool   `json:"is_server"`
}

type FriendRequest struct {
	ID        string `json:"id"`
	From      Device `json:"from"`
	CreatedAt int64  `json:"created_at"`
}

type PairingResult struct {
	Device Device
}

type Conversation struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	Avatar         string `json:"avatar"`
	CreatedAt      int64  `json:"created_at"`
	LastSeenAt     int64  `json:"last_seen_at,omitempty"`
	LastMessageAt  int64  `json:"last_message_at"`
	LastKind       string `json:"last_kind,omitempty"`
	LastPreview    string `json:"last_preview,omitempty"`
	MemberCount    int    `json:"member_count,omitempty"`
	IsServer       bool   `json:"is_server"`
}

func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	databasePath := filepath.Join(dataDir, "selfsend.db")
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", databasePath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.ensureServerSettings(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
	device_id TEXT NOT NULL DEFAULT '',
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS uploads (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL DEFAULT '',
	sender_device_id TEXT NOT NULL DEFAULT '',
  file_name TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  total_size INTEGER NOT NULL,
  offset INTEGER NOT NULL DEFAULT 0,
  temp_path TEXT NOT NULL,
  last_modified INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS items (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL DEFAULT '',
	sender_device_id TEXT NOT NULL DEFAULT '',
  file_name TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  size INTEGER NOT NULL,
  sha256 TEXT NOT NULL,
  storage_path TEXT NOT NULL,
  last_modified INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS notes (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL DEFAULT '',
	sender_device_id TEXT NOT NULL DEFAULT '',
  text TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS devices (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  avatar TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS pairing_invites (
  token_hash TEXT PRIMARY KEY,
  inviter_device_id TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  consumed_at INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY(inviter_device_id) REFERENCES devices(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS friend_requests (
  id TEXT PRIMARY KEY,
  from_device_id TEXT NOT NULL,
  to_device_id TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  created_at INTEGER NOT NULL,
  UNIQUE(from_device_id, to_device_id),
  FOREIGN KEY(from_device_id) REFERENCES devices(id) ON DELETE CASCADE,
  FOREIGN KEY(to_device_id) REFERENCES devices(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS friendships (
  conversation_id TEXT PRIMARY KEY,
  device_a_id TEXT NOT NULL,
  device_b_id TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE(device_a_id, device_b_id),
  FOREIGN KEY(device_a_id) REFERENCES devices(id) ON DELETE CASCADE,
  FOREIGN KEY(device_b_id) REFERENCES devices(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS groups (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  avatar TEXT NOT NULL DEFAULT '群',
  created_by_device_id TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  FOREIGN KEY(created_by_device_id) REFERENCES devices(id) ON DELETE RESTRICT
);
CREATE TABLE IF NOT EXISTS group_members (
  group_id TEXT NOT NULL,
  device_id TEXT NOT NULL,
  joined_at INTEGER NOT NULL,
  PRIMARY KEY(group_id, device_id),
  FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE,
  FOREIGN KEY(device_id) REFERENCES devices(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS handoff_claims (
  nonce TEXT PRIMARY KEY,
  device_id TEXT NOT NULL,
  claimed_at INTEGER NOT NULL,
  FOREIGN KEY(device_id) REFERENCES devices(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	for _, migration := range []struct {
		table, column, definition string
	}{
		{"uploads", "conversation_id", "TEXT NOT NULL DEFAULT ''"},
		{"items", "conversation_id", "TEXT NOT NULL DEFAULT ''"},
		{"notes", "conversation_id", "TEXT NOT NULL DEFAULT ''"},
		{"sessions", "device_id", "TEXT NOT NULL DEFAULT ''"},
		{"uploads", "sender_device_id", "TEXT NOT NULL DEFAULT ''"},
		{"items", "sender_device_id", "TEXT NOT NULL DEFAULT ''"},
		{"notes", "sender_device_id", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.ensureColumn(ctx, migration.table, migration.column, migration.definition); err != nil {
			return fmt.Errorf("migrate %s.%s: %w", migration.table, migration.column, err)
		}
	}
	if err := s.normalizeDeviceNames(ctx); err != nil {
		return fmt.Errorf("normalize device names: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
CREATE INDEX IF NOT EXISTS idx_items_timeline ON items(conversation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notes_timeline ON notes(conversation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_devices_seen ON devices(last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_group_members_device ON group_members(device_id, group_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_name_unique ON devices(name COLLATE NOCASE);
UPDATE items SET storage_path = 'blobs/' || id || '/data' WHERE storage_path != '';
INSERT OR IGNORE INTO friendships(conversation_id, device_a_id, device_b_id, created_at)
SELECT lower(hex(randomblob(16))), a.id, b.id, MAX(a.created_at, b.created_at)
FROM devices a JOIN devices b ON a.id < b.id;
INSERT OR IGNORE INTO settings(key, value)
SELECT 'server_device_id', id FROM devices ORDER BY created_at, id LIMIT 1;`); err != nil {
		return fmt.Errorf("create timeline indexes: %w", err)
	}
	return nil
}

func (s *Store) normalizeDeviceNames(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM devices ORDER BY created_at, id`)
	if err != nil {
		return err
	}
	type namedDevice struct{ id, name string }
	devices := make([]namedDevice, 0)
	for rows.Next() {
		var device namedDevice
		if err := rows.Scan(&device.id, &device.name); err != nil {
			rows.Close()
			return err
		}
		devices = append(devices, device)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	used := make(map[string]struct{}, len(devices))
	for _, device := range devices {
		name := uniqueNameFromSet(device.name, used)
		used[strings.ToLower(name)] = struct{}{}
		if name != device.name {
			if _, err := tx.ExecContext(ctx, `UPDATE devices SET name = ? WHERE id = ?`, name, device.id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func uniqueNameFromSet(base string, used map[string]struct{}) string {
	if _, exists := used[strings.ToLower(base)]; !exists {
		return base
	}
	for number := 2; ; number++ {
		candidate := deviceNameWithSuffix(base, number)
		if _, exists := used[strings.ToLower(candidate)]; !exists {
			return candidate
		}
	}
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func availableDeviceName(ctx context.Context, query queryRower, base, excludeID string) (string, error) {
	for number := 1; ; number++ {
		candidate := base
		if number > 1 {
			candidate = deviceNameWithSuffix(base, number)
		}
		var count int
		if err := query.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE name = ? COLLATE NOCASE AND id != ?`, candidate, excludeID).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
	}
}

func deviceNameWithSuffix(base string, number int) string {
	suffix := fmt.Sprintf("(%d)", number)
	runes := []rune(base)
	if keep := 40 - len([]rune(suffix)); len(runes) > keep {
		runes = runes[:keep]
	}
	return string(runes) + suffix
}

func (s *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			rows.Close()
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	_, err = s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

func (s *Store) IsSetup(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings WHERE key = 'password_hash'`).Scan(&count)
	return count > 0, err
}

func (s *Store) InitializePassword(ctx context.Context, passwordHash string) error {
	result, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO settings(key, value) VALUES('password_hash', ?)`, passwordHash)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errors.New("instance is already initialized")
	}
	return nil
}

func (s *Store) PasswordHash(ctx context.Context) (string, error) {
	var hash string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'password_hash'`).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return hash, nil
}

func (s *Store) CreateSession(ctx context.Context, tokenHash, deviceID string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(token_hash, device_id, expires_at) VALUES(?, ?, ?)`, tokenHash, deviceID, expiresAt.Unix())
	return err
}

func (s *Store) SessionValid(ctx context.Context, tokenHash string, now time.Time) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE token_hash = ? AND expires_at > ?`, tokenHash, now.Unix()).Scan(&count)
	return count > 0, err
}

func (s *Store) SessionDevice(ctx context.Context, tokenHash string, now time.Time) (string, error) {
	var deviceID string
	err := s.db.QueryRowContext(ctx, `SELECT device_id FROM sessions WHERE token_hash = ? AND expires_at > ?`, tokenHash, now.Unix()).Scan(&deviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return deviceID, err
}

func (s *Store) BindSessionDevice(ctx context.Context, tokenHash, deviceID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET device_id = ? WHERE token_hash = ?`, deviceID, tokenHash)
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

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *Store) PurgeExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.Unix())
	return err
}

func (s *Store) CreateUpload(ctx context.Context, upload Upload) error {
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO uploads(id, conversation_id, sender_device_id, file_name, mime_type, total_size, offset, temp_path, last_modified, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`, upload.ID, upload.ConversationID, upload.SenderDeviceID, upload.FileName, upload.MimeType, upload.TotalSize, upload.TempPath, upload.LastModified, now, now)
	return err
}

func (s *Store) Upload(ctx context.Context, id string) (Upload, error) {
	var upload Upload
	err := s.db.QueryRowContext(ctx, `
SELECT id, conversation_id, sender_device_id, file_name, mime_type, total_size, offset, temp_path, last_modified FROM uploads WHERE id = ?`, id).
		Scan(&upload.ID, &upload.ConversationID, &upload.SenderDeviceID, &upload.FileName, &upload.MimeType, &upload.TotalSize, &upload.Offset, &upload.TempPath, &upload.LastModified)
	if errors.Is(err, sql.ErrNoRows) {
		return Upload{}, ErrNotFound
	}
	return upload, err
}

func (s *Store) UpdateUploadOffset(ctx context.Context, id string, expectedOffset, nextOffset int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE uploads SET offset = ?, updated_at = ? WHERE id = ? AND offset = ?`, nextOffset, time.Now().UnixMilli(), id, expectedOffset)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("upload offset changed concurrently")
	}
	return nil
}

func (s *Store) CompleteUpload(ctx context.Context, upload Upload, sha256, storagePath string) error {
	createdAt := s.nextTimelineTimestamp(ctx)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO items(id, conversation_id, sender_device_id, file_name, mime_type, size, sha256, storage_path, last_modified, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, upload.ID, upload.ConversationID, upload.SenderDeviceID, upload.FileName, upload.MimeType, upload.TotalSize, sha256, storagePath, upload.LastModified, createdAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM uploads WHERE id = ?`, upload.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteUpload(ctx context.Context, id string) (string, error) {
	upload, err := s.Upload(ctx, id)
	if err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM uploads WHERE id = ?`, id); err != nil {
		return "", err
	}
	return upload.TempPath, nil
}

func (s *Store) CreateNote(ctx context.Context, id, conversationID, senderDeviceID, text string) (TimelineItem, error) {
	createdAt := s.nextTimelineTimestamp(ctx)
	_, err := s.db.ExecContext(ctx, `INSERT INTO notes(id, conversation_id, sender_device_id, text, created_at) VALUES(?, ?, ?, ?, ?)`, id, conversationID, senderDeviceID, text, createdAt)
	if err != nil {
		return TimelineItem{}, err
	}
	return TimelineItem{Kind: "text", ID: id, Text: text, CreatedAt: createdAt, SenderDeviceID: senderDeviceID}, nil
}

func (s *Store) nextTimelineTimestamp(ctx context.Context) int64 {
	s.timelineMu.Lock()
	defer s.timelineMu.Unlock()
	if s.lastTimelineAt == 0 {
		_ = s.db.QueryRowContext(ctx, `
SELECT MAX(created_at) FROM (
  SELECT created_at FROM items
  UNION ALL
  SELECT created_at FROM notes
)`).Scan(&s.lastTimelineAt)
	}
	now := time.Now().UnixMilli()
	if now <= s.lastTimelineAt {
		now = s.lastTimelineAt + 1
	}
	s.lastTimelineAt = now
	return now
}

func (s *Store) ListTimeline(ctx context.Context, conversationID string, before int64, limit int) ([]TimelineItem, error) {
	if before <= 0 {
		before = time.Now().Add(24 * time.Hour).UnixMilli()
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT t.kind, t.id, t.text, t.file_name, t.mime_type, t.size, t.sha256, t.last_modified, t.created_at,
       t.sender_device_id, COALESCE(d.name, ''), COALESCE(d.avatar, '')
FROM (
  SELECT 'file' AS kind, id, '' AS text, file_name, mime_type, size, sha256, last_modified, created_at, sender_device_id FROM items WHERE conversation_id = ?
  UNION ALL
  SELECT 'text' AS kind, id, text, '' AS file_name, '' AS mime_type, 0 AS size, '' AS sha256, 0 AS last_modified, created_at, sender_device_id FROM notes WHERE conversation_id = ?
) t LEFT JOIN devices d ON d.id = t.sender_device_id
WHERE t.created_at < ? ORDER BY t.created_at DESC LIMIT ?`, conversationID, conversationID, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TimelineItem, 0, limit)
	for rows.Next() {
		var item TimelineItem
		if err := rows.Scan(&item.Kind, &item.ID, &item.Text, &item.FileName, &item.MimeType, &item.Size, &item.SHA256, &item.LastModified, &item.CreatedAt, &item.SenderDeviceID, &item.SenderName, &item.SenderAvatar); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Item(ctx context.Context, id string) (Item, error) {
	var item Item
	err := s.db.QueryRowContext(ctx, `
SELECT id, conversation_id, sender_device_id, file_name, mime_type, size, sha256, storage_path, last_modified, created_at FROM items WHERE id = ?`, id).
		Scan(&item.ID, &item.ConversationID, &item.SenderDeviceID, &item.FileName, &item.MimeType, &item.Size, &item.SHA256, &item.StoragePath, &item.LastModified, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	return item, err
}

func (s *Store) DeleteItem(ctx context.Context, id string) (string, error) {
	item, err := s.Item(ctx, id)
	if err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, id); err != nil {
		return "", err
	}
	return item.StoragePath, nil
}

func (s *Store) DeleteTimelineItem(ctx context.Context, id string) (storagePath string, isFile bool, err error) {
	storagePath, err = s.DeleteItem(ctx, id)
	if err == nil {
		return storagePath, true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", false, err
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM notes WHERE id = ?`, id)
	if err != nil {
		return "", false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return "", false, err
	}
	if affected == 0 {
		return "", false, ErrNotFound
	}
	return "", false, nil
}

func (s *Store) Stats(ctx context.Context) (int64, int64, error) {
	var count, size int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(size), 0) FROM items`).Scan(&count, &size)
	return count, size, err
}

func (s *Store) RegisterDevice(ctx context.Context, id, name, avatar string) (Device, error) {
	now := time.Now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Device{}, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE id = ?`, id).Scan(&exists); err != nil {
		return Device{}, err
	}
	if exists == 0 {
		name, err = availableDeviceName(ctx, tx, name, "")
		if err != nil {
			return Device{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO devices(id, name, avatar, created_at, last_seen_at) VALUES(?, ?, ?, ?, ?)`, id, name, avatar, now, now); err != nil {
			return Device{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO settings(key, value) VALUES('server_device_id', ?)`, id); err != nil {
			return Device{}, err
		}
		for _, table := range []string{"uploads", "items", "notes"} {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET conversation_id = ? WHERE conversation_id = ''", table), id); err != nil {
				return Device{}, err
			}
		}
		if err := connectDeviceToAll(ctx, tx, id, now); err != nil {
			return Device{}, err
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE devices SET last_seen_at = ? WHERE id = ?`, now, id); err != nil {
		return Device{}, err
	}
	var device Device
	if err := tx.QueryRowContext(ctx, `
SELECT d.id, d.name, d.avatar, d.created_at, d.last_seen_at,
       EXISTS(SELECT 1 FROM settings s WHERE s.key = 'server_device_id' AND s.value = d.id)
FROM devices d WHERE d.id = ?`, id).
		Scan(&device.ID, &device.Name, &device.Avatar, &device.CreatedAt, &device.LastSeenAt, &device.IsServer); err != nil {
		return Device{}, err
	}
	if err := tx.Commit(); err != nil {
		return Device{}, err
	}
	return device, nil
}

func (s *Store) Device(ctx context.Context, id string) (Device, error) {
	var device Device
	err := s.db.QueryRowContext(ctx, `
SELECT d.id, d.name, d.avatar, d.created_at, d.last_seen_at,
       EXISTS(SELECT 1 FROM settings s WHERE s.key = 'server_device_id' AND s.value = d.id)
FROM devices d WHERE d.id = ?`, id).
		Scan(&device.ID, &device.Name, &device.Avatar, &device.CreatedAt, &device.LastSeenAt, &device.IsServer)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return device, err
}

func (s *Store) UpdateDevice(ctx context.Context, id, name, avatar string) (Device, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Device{}, err
	}
	defer tx.Rollback()
	name, err = availableDeviceName(ctx, tx, name, id)
	if err != nil {
		return Device{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE devices SET name = ?, avatar = ? WHERE id = ?`, name, avatar, id)
	if err != nil {
		return Device{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Device{}, err
	}
	if affected == 0 {
		return Device{}, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return Device{}, err
	}
	return s.Device(ctx, id)
}

func (s *Store) CreatePairingInvite(ctx context.Context, tokenHash, inviterDeviceID string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO pairing_invites(token_hash, inviter_device_id, expires_at) VALUES(?, ?, ?)`, tokenHash, inviterDeviceID, expiresAt.Unix())
	return err
}

func (s *Store) ClaimPairingInvite(ctx context.Context, tokenHash, deviceID, name, avatar string, now time.Time) (PairingResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PairingResult{}, err
	}
	defer tx.Rollback()
	var inviterID string
	err = tx.QueryRowContext(ctx, `SELECT inviter_device_id FROM pairing_invites WHERE token_hash = ? AND expires_at > ? AND consumed_at = 0`, tokenHash, now.Unix()).Scan(&inviterID)
	if errors.Is(err, sql.ErrNoRows) {
		return PairingResult{}, ErrNotFound
	}
	if err != nil {
		return PairingResult{}, err
	}
	if inviterID == deviceID {
		return PairingResult{}, errors.New("cannot pair a device with itself")
	}
	nowMillis := now.UnixMilli()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE id = ?`, deviceID).Scan(&exists); err != nil {
		return PairingResult{}, err
	}
	if exists == 0 {
		name, err = availableDeviceName(ctx, tx, name, "")
		if err != nil {
			return PairingResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO devices(id, name, avatar, created_at, last_seen_at) VALUES(?, ?, ?, ?, ?)`, deviceID, name, avatar, nowMillis, nowMillis); err != nil {
			return PairingResult{}, err
		}
		if err := connectDeviceToAll(ctx, tx, deviceID, nowMillis); err != nil {
			return PairingResult{}, err
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE devices SET last_seen_at = ? WHERE id = ?`, nowMillis, deviceID); err != nil {
		return PairingResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pairing_invites SET consumed_at = ? WHERE token_hash = ? AND consumed_at = 0`, nowMillis, tokenHash); err != nil {
		return PairingResult{}, err
	}
	var device Device
	if err := tx.QueryRowContext(ctx, `
SELECT d.id, d.name, d.avatar, d.created_at, d.last_seen_at,
       EXISTS(SELECT 1 FROM settings s WHERE s.key = 'server_device_id' AND s.value = d.id)
FROM devices d WHERE d.id = ?`, deviceID).
		Scan(&device.ID, &device.Name, &device.Avatar, &device.CreatedAt, &device.LastSeenAt, &device.IsServer); err != nil {
		return PairingResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return PairingResult{}, err
	}
	return PairingResult{Device: device}, nil
}

func connectDeviceToAll(ctx context.Context, tx *sql.Tx, deviceID string, now int64) error {
	_, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO friendships(conversation_id, device_a_id, device_b_id, created_at)
SELECT lower(hex(randomblob(16))),
       CASE WHEN id < ? THEN id ELSE ? END,
       CASE WHEN id < ? THEN ? ELSE id END,
       ?
FROM devices WHERE id != ?`, deviceID, deviceID, deviceID, deviceID, now, deviceID)
	return err
}

func (s *Store) ListFriendRequests(ctx context.Context, deviceID string) ([]FriendRequest, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT r.id, d.id, d.name, d.avatar, d.created_at, d.last_seen_at, r.created_at
FROM friend_requests r JOIN devices d ON d.id = r.from_device_id
WHERE r.to_device_id = ? AND r.status = 'pending' ORDER BY r.created_at DESC`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	requests := make([]FriendRequest, 0)
	for rows.Next() {
		var request FriendRequest
		if err := rows.Scan(&request.ID, &request.From.ID, &request.From.Name, &request.From.Avatar, &request.From.CreatedAt, &request.From.LastSeenAt, &request.CreatedAt); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, rows.Err()
}

func (s *Store) AcceptFriendRequest(ctx context.Context, requestID, acceptingDeviceID, conversationID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var fromID, toID, status string
	if err := tx.QueryRowContext(ctx, `SELECT from_device_id, to_device_id, status FROM friend_requests WHERE id = ?`, requestID).Scan(&fromID, &toID, &status); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if toID != acceptingDeviceID || status != "pending" {
		return ErrNotFound
	}
	deviceA, deviceB := fromID, toID
	if deviceA > deviceB {
		deviceA, deviceB = deviceB, deviceA
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO friendships(conversation_id, device_a_id, device_b_id, created_at) VALUES(?, ?, ?, ?)`, conversationID, deviceA, deviceB, time.Now().UnixMilli()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE friend_requests SET status = 'accepted' WHERE id = ?`, requestID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ConversationMember(ctx context.Context, conversationID, deviceID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
SELECT
  (SELECT COUNT(*) FROM friendships WHERE conversation_id = ? AND (device_a_id = ? OR device_b_id = ?)) +
  (SELECT COUNT(*) FROM groups g JOIN group_members gm ON gm.group_id = g.id WHERE g.conversation_id = ? AND gm.device_id = ?)`,
		conversationID, deviceID, deviceID, conversationID, deviceID).Scan(&count)
	return count > 0, err
}

func (s *Store) CreateGroup(ctx context.Context, id, conversationID, creatorDeviceID string, memberIDs []string) (Conversation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Conversation{}, err
	}
	defer tx.Rollback()
	allIDs := make([]string, 0, len(memberIDs)+1)
	seen := map[string]struct{}{creatorDeviceID: {}}
	allIDs = append(allIDs, creatorDeviceID)
	for _, memberID := range memberIDs {
		if _, exists := seen[memberID]; exists {
			continue
		}
		seen[memberID] = struct{}{}
		allIDs = append(allIDs, memberID)
	}
	if len(allIDs) < 3 {
		return Conversation{}, errors.New("a group requires at least three devices")
	}
	names := make([]string, 0, len(allIDs))
	for _, deviceID := range allIDs {
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM devices WHERE id = ?`, deviceID).Scan(&name); errors.Is(err, sql.ErrNoRows) {
			return Conversation{}, ErrNotFound
		} else if err != nil {
			return Conversation{}, err
		}
		names = append(names, name)
	}
	nameRunes := []rune(strings.Join(names, "、"))
	if len(nameRunes) > 40 {
		nameRunes = nameRunes[:39]
		nameRunes = append(nameRunes, '…')
	}
	name := string(nameRunes)
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `INSERT INTO groups(id, conversation_id, name, avatar, created_by_device_id, created_at) VALUES(?, ?, ?, '群', ?, ?)`, id, conversationID, name, creatorDeviceID, now); err != nil {
		return Conversation{}, err
	}
	for _, deviceID := range allIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO group_members(group_id, device_id, joined_at) VALUES(?, ?, ?)`, id, deviceID, now); err != nil {
			return Conversation{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Conversation{}, err
	}
	return Conversation{ID: id, ConversationID: conversationID, Kind: "group", Name: name, Avatar: "群", CreatedAt: now, MemberCount: len(allIDs)}, nil
}

func (s *Store) ListConversations(ctx context.Context, currentDeviceID string) ([]Conversation, error) {
	rows, err := s.db.QueryContext(ctx, `
WITH timeline AS (
  SELECT conversation_id, created_at, 'file' AS kind, file_name AS preview FROM items
  UNION ALL
  SELECT conversation_id, created_at, 'text' AS kind, text AS preview FROM notes
), ranked AS (
  SELECT conversation_id, created_at, kind, preview,
         ROW_NUMBER() OVER (PARTITION BY conversation_id ORDER BY created_at DESC) AS rank
  FROM timeline
), friends AS (
  SELECT conversation_id, CASE WHEN device_a_id = ? THEN device_b_id ELSE device_a_id END AS device_id
  FROM friendships WHERE device_a_id = ? OR device_b_id = ?
), chats AS (
  SELECT d.id, f.conversation_id, 'direct' AS kind, d.name, d.avatar, d.created_at, d.last_seen_at,
         0 AS member_count,
         EXISTS(SELECT 1 FROM settings s WHERE s.key = 'server_device_id' AND s.value = d.id) AS is_server
  FROM friends f JOIN devices d ON d.id = f.device_id
  UNION ALL
  SELECT g.id, g.conversation_id, 'group' AS kind, g.name, g.avatar, g.created_at, 0 AS last_seen_at,
         (SELECT COUNT(*) FROM group_members members WHERE members.group_id = g.id) AS member_count,
         0 AS is_server
  FROM groups g JOIN group_members gm ON gm.group_id = g.id WHERE gm.device_id = ?
)
SELECT c.id, c.conversation_id, c.kind, c.name, c.avatar, c.created_at, c.last_seen_at,
       COALESCE(r.created_at, 0), COALESCE(r.kind, ''), COALESCE(r.preview, '')
       , c.member_count, c.is_server
FROM chats c
LEFT JOIN ranked r ON r.conversation_id = c.conversation_id AND r.rank = 1
ORDER BY CASE WHEN r.created_at IS NULL THEN c.created_at ELSE r.created_at END DESC, c.name ASC`, currentDeviceID, currentDeviceID, currentDeviceID, currentDeviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	conversations := make([]Conversation, 0)
	for rows.Next() {
		var conversation Conversation
		if err := rows.Scan(&conversation.ID, &conversation.ConversationID, &conversation.Kind, &conversation.Name, &conversation.Avatar, &conversation.CreatedAt, &conversation.LastSeenAt, &conversation.LastMessageAt, &conversation.LastKind, &conversation.LastPreview, &conversation.MemberCount, &conversation.IsServer); err != nil {
			return nil, err
		}
		conversations = append(conversations, conversation)
	}
	return conversations, rows.Err()
}

func (s *Store) TimelineItemSender(ctx context.Context, id string) (string, error) {
	var sender string
	err := s.db.QueryRowContext(ctx, `SELECT sender_device_id FROM items WHERE id = ? UNION ALL SELECT sender_device_id FROM notes WHERE id = ? LIMIT 1`, id, id).Scan(&sender)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return sender, err
}
