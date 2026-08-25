package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

type Upload struct {
	ID           string
	FileName     string
	MimeType     string
	TotalSize    int64
	Offset       int64
	TempPath     string
	LastModified int64
}

type Item struct {
	ID           string `json:"id"`
	FileName     string `json:"file_name"`
	MimeType     string `json:"mime_type"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	CreatedAt    int64  `json:"created_at"`
	LastModified int64  `json:"last_modified,omitempty"`
	StoragePath  string `json:"-"`
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
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS uploads (
  id TEXT PRIMARY KEY,
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
  file_name TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  size INTEGER NOT NULL,
  sha256 TEXT NOT NULL,
  storage_path TEXT NOT NULL,
  last_modified INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_items_created_at ON items(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	return nil
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

func (s *Store) CreateSession(ctx context.Context, tokenHash string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(token_hash, expires_at) VALUES(?, ?)`, tokenHash, expiresAt.Unix())
	return err
}

func (s *Store) SessionValid(ctx context.Context, tokenHash string, now time.Time) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE token_hash = ? AND expires_at > ?`, tokenHash, now.Unix()).Scan(&count)
	return count > 0, err
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
INSERT INTO uploads(id, file_name, mime_type, total_size, offset, temp_path, last_modified, created_at, updated_at)
VALUES(?, ?, ?, ?, 0, ?, ?, ?, ?)`, upload.ID, upload.FileName, upload.MimeType, upload.TotalSize, upload.TempPath, upload.LastModified, now, now)
	return err
}

func (s *Store) Upload(ctx context.Context, id string) (Upload, error) {
	var upload Upload
	err := s.db.QueryRowContext(ctx, `
SELECT id, file_name, mime_type, total_size, offset, temp_path, last_modified FROM uploads WHERE id = ?`, id).
		Scan(&upload.ID, &upload.FileName, &upload.MimeType, &upload.TotalSize, &upload.Offset, &upload.TempPath, &upload.LastModified)
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO items(id, file_name, mime_type, size, sha256, storage_path, last_modified, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, upload.ID, upload.FileName, upload.MimeType, upload.TotalSize, sha256, storagePath, upload.LastModified, time.Now().UnixMilli()); err != nil {
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

func (s *Store) ListItems(ctx context.Context, before int64, limit int) ([]Item, error) {
	if before <= 0 {
		before = time.Now().Add(24 * time.Hour).UnixMilli()
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, file_name, mime_type, size, sha256, storage_path, last_modified, created_at
FROM items WHERE created_at < ? ORDER BY created_at DESC LIMIT ?`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Item, 0, limit)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.FileName, &item.MimeType, &item.Size, &item.SHA256, &item.StoragePath, &item.LastModified, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Item(ctx context.Context, id string) (Item, error) {
	var item Item
	err := s.db.QueryRowContext(ctx, `
SELECT id, file_name, mime_type, size, sha256, storage_path, last_modified, created_at FROM items WHERE id = ?`, id).
		Scan(&item.ID, &item.FileName, &item.MimeType, &item.Size, &item.SHA256, &item.StoragePath, &item.LastModified, &item.CreatedAt)
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

func (s *Store) Stats(ctx context.Context) (int64, int64, error) {
	var count, size int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(size), 0) FROM items`).Scan(&count, &size)
	return count, size, err
}
