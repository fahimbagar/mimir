// Package store keeps users, login sessions and reading progress in SQLite.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go, no cgo)
)

// ErrNotFound is returned for unknown or expired sessions.
var ErrNotFound = errors.New("not found")

// Each entry upgrades the schema by one version (PRAGMA user_version).
var migrations = []string{
	`CREATE TABLE users (
		email      TEXT PRIMARY KEY,
		name       TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		last_login INTEGER NOT NULL
	);
	CREATE TABLE sessions (
		token_hash BLOB PRIMARY KEY,
		email      TEXT NOT NULL REFERENCES users(email) ON DELETE CASCADE,
		created_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL
	);
	CREATE INDEX sessions_expires ON sessions(expires_at);
	CREATE TABLE progress (
		email      TEXT NOT NULL REFERENCES users(email) ON DELETE CASCADE,
		file_id    TEXT NOT NULL,
		position   TEXT NOT NULL,
		progress   REAL NOT NULL,
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (email, file_id)
	);`,
}

// Store wraps the SQLite database.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens (creating if needed) the database at path and applies
// migrations.
func Open(path string) (*Store, error) {
	// The DSN is "file:<path>?<pragmas>", so the path itself can't hold them.
	if strings.ContainsAny(path, "?#") {
		return nil, fmt.Errorf("database path %q must not contain '?' or '#'", path)
	}
	dsn := "file:" + path +
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection serialises writes, so SQLITE_BUSY can't happen. Traffic
	// is a handful of readers, so there is nothing to gain from a pool.
	db.SetMaxOpenConns(1)
	s := &Store{db: db, now: time.Now}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		// PRAGMA doesn't take bind parameters; i is our own loop counter.
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// UpsertUser records a successful login.
func (s *Store) UpsertUser(ctx context.Context, email, name string) error {
	now := s.now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (email, name, created_at, last_login) VALUES (?, ?, ?, ?)
		ON CONFLICT(email) DO UPDATE SET name = excluded.name, last_login = excluded.last_login`,
		email, name, now, now)
	return err
}

// CreateSession starts a session for email and returns the raw token for the
// cookie. Only its SHA-256 is stored, so a leaked database can't be replayed
// as logins.
func (s *Store) CreateSession(ctx context.Context, email string, ttl time.Duration) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.Unix()); err != nil {
		return "", err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, email, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		hashToken(token), email, now.Unix(), now.Add(ttl).Unix())
	if err != nil {
		return "", err
	}
	return token, nil
}

// SessionEmail returns the user of a live session.
func (s *Store) SessionEmail(ctx context.Context, token string) (string, error) {
	var email string
	err := s.db.QueryRowContext(ctx,
		`SELECT email FROM sessions WHERE token_hash = ? AND expires_at > ?`,
		hashToken(token), s.now().Unix()).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return email, err
}

// DeleteSession ends a session. Unknown tokens are not an error.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

// Progress is where a user stopped in one file.
type Progress struct {
	FileID string `json:"fileId"`
	// Position is JSON: a page number for PDFs, a CFI string for EPUBs.
	Position  string    `json:"position"`
	Progress  float64   `json:"progress"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SaveProgress records a reading position.
func (s *Store) SaveProgress(ctx context.Context, email string, p Progress) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO progress (email, file_id, position, progress, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(email, file_id) DO UPDATE SET
			position = excluded.position, progress = excluded.progress, updated_at = excluded.updated_at`,
		email, p.FileID, p.Position, p.Progress, s.now().UnixMilli())
	return err
}

// ListProgress returns a user's positions, most recently read first.
func (s *Store) ListProgress(ctx context.Context, email string, limit int) ([]Progress, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT file_id, position, progress, updated_at FROM progress
		WHERE email = ? ORDER BY updated_at DESC LIMIT ?`, email, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Progress{}
	for rows.Next() {
		var p Progress
		var updated int64
		if err := rows.Scan(&p.FileID, &p.Position, &p.Progress, &updated); err != nil {
			return nil, err
		}
		p.UpdatedAt = time.UnixMilli(updated).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
