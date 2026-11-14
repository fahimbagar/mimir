package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for range 2 {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessions(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	now := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return now }

	if err := s.UpsertUser(ctx, "a@example.com", "A"); err != nil {
		t.Fatal(err)
	}
	token, err := s.CreateSession(ctx, "a@example.com", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 40 {
		t.Errorf("token %q looks too short", token)
	}

	email, err := s.SessionEmail(ctx, token)
	if err != nil || email != "a@example.com" {
		t.Fatalf("SessionEmail = %q, %v", email, err)
	}
	if _, err := s.SessionEmail(ctx, token+"x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("wrong token: %v", err)
	}

	// The raw token must not be stored.
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM sessions WHERE token_hash = ?`, token).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("raw token found in the database")
	}

	now = now.Add(2 * time.Hour)
	if _, err := s.SessionEmail(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired session: %v", err)
	}

	now = now.Add(-2 * time.Hour)
	if err := s.DeleteSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionEmail(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted session: %v", err)
	}
}

func TestCreateSessionRequiresUser(t *testing.T) {
	s := openTest(t)
	if _, err := s.CreateSession(context.Background(), "nobody@example.com", time.Hour); err == nil {
		t.Error("created a session for an unknown user")
	}
}

func TestProgress(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	now := time.UnixMilli(1_800_000_000_000)
	s.now = func() time.Time { return now }
	for _, email := range []string{"a@example.com", "b@example.com"} {
		if err := s.UpsertUser(ctx, email, ""); err != nil {
			t.Fatal(err)
		}
	}

	save := func(email, file, pos string, pct float64) {
		t.Helper()
		now = now.Add(time.Second)
		if err := s.SaveProgress(ctx, email, Progress{FileID: file, Position: pos, Progress: pct}); err != nil {
			t.Fatal(err)
		}
	}
	save("a@example.com", "f1", "12", 0.1)
	save("a@example.com", "f2", `"epubcfi(/6/4)"`, 0.5)
	save("a@example.com", "f1", "40", 0.4) // update moves f1 to the front
	save("b@example.com", "f3", "1", 0)

	list, err := s.ListProgress(ctx, "a@example.com", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].FileID != "f1" || list[0].Position != "40" || list[0].Progress != 0.4 || list[1].FileID != "f2" {
		t.Fatalf("unexpected progress: %+v", list)
	}
	if !list[0].UpdatedAt.After(list[1].UpdatedAt) {
		t.Error("not ordered by recency")
	}

	limited, err := s.ListProgress(ctx, "a@example.com", 1)
	if err != nil || len(limited) != 1 {
		t.Fatalf("limit: %+v %v", limited, err)
	}
	empty, err := s.ListProgress(ctx, "nobody@example.com", 10)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("unknown user should give an empty list: %+v %v", empty, err)
	}
}
