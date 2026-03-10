package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestStore creates a Store backed by a temp file that is cleaned up after t.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	f := filepath.Join(t.TempDir(), "state.json")
	s, err := New(f)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// ---- Reserve ---------------------------------------------------------------

func TestReserve_Success(t *testing.T) {
	s := newTestStore(t)

	r, err := s.Reserve("X1", "owner-a", time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Token == "" {
		t.Error("expected non-empty token")
	}
	if r.ResourceID != "X1" {
		t.Errorf("resource_id = %q, want X1", r.ResourceID)
	}
	if r.Owner != "owner-a" {
		t.Errorf("owner = %q, want owner-a", r.Owner)
	}
	if r.ExpiresAt.Before(time.Now()) {
		t.Error("expires_at is already in the past")
	}
}

func TestReserve_Locked(t *testing.T) {
	s := newTestStore(t)

	_, err := s.Reserve("X1", "owner-a", time.Hour)
	if err != nil {
		t.Fatalf("first reserve failed: %v", err)
	}

	_, err = s.Reserve("X1", "owner-b", time.Hour)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var locked *ErrResourceLocked
	if !errors.As(err, &locked) {
		t.Fatalf("expected ErrResourceLocked, got %T: %v", err, err)
	}
	if locked.Owner != "owner-a" {
		t.Errorf("locked.Owner = %q, want owner-a", locked.Owner)
	}
	if locked.Remaining <= 0 {
		t.Error("expected positive remaining duration")
	}
}

func TestReserve_AfterExpiry(t *testing.T) {
	s := newTestStore(t)

	// Reserve with a duration that already expired.
	_, err := s.Reserve("X1", "owner-a", -time.Second)
	if err != nil {
		t.Fatalf("first reserve failed: %v", err)
	}

	// Should succeed now because the previous one is expired.
	r, err := s.Reserve("X1", "owner-b", time.Hour)
	if err != nil {
		t.Fatalf("second reserve after expiry failed: %v", err)
	}
	if r.Owner != "owner-b" {
		t.Errorf("owner = %q, want owner-b", r.Owner)
	}
}

func TestReserve_DifferentResources(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.Reserve("X1", "owner-a", time.Hour); err != nil {
		t.Fatalf("reserve X1: %v", err)
	}
	if _, err := s.Reserve("X2", "owner-b", time.Hour); err != nil {
		t.Fatalf("reserve X2: %v", err)
	}
}

// ---- Extend ----------------------------------------------------------------

func TestExtend_Success(t *testing.T) {
	s := newTestStore(t)

	r, _ := s.Reserve("X1", "owner-a", time.Hour)
	before := r.ExpiresAt

	updated, err := s.Extend(r.Token, 30*time.Minute)
	if err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if !updated.ExpiresAt.After(before) {
		t.Errorf("expires_at not extended: before=%v after=%v", before, updated.ExpiresAt)
	}
}

func TestExtend_UnknownToken(t *testing.T) {
	s := newTestStore(t)

	_, err := s.Extend("no-such-token", time.Hour)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestExtend_ExpiredReservation(t *testing.T) {
	s := newTestStore(t)

	r, _ := s.Reserve("X1", "owner-a", -time.Second)

	_, err := s.Extend(r.Token, time.Hour)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for expired token, got %v", err)
	}
}

// ---- Release ---------------------------------------------------------------

func TestRelease_Success(t *testing.T) {
	s := newTestStore(t)

	r, _ := s.Reserve("X1", "owner-a", time.Hour)

	if err := s.Release(r.Token); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// Resource should now be available.
	if s.Status("X1") != nil {
		t.Error("resource still appears reserved after release")
	}
}

func TestRelease_UnknownToken(t *testing.T) {
	s := newTestStore(t)

	if err := s.Release("no-such-token"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRelease_AllowsReReservation(t *testing.T) {
	s := newTestStore(t)

	r, _ := s.Reserve("X1", "owner-a", time.Hour)
	_ = s.Release(r.Token)

	if _, err := s.Reserve("X1", "owner-b", time.Hour); err != nil {
		t.Fatalf("re-reservation after release failed: %v", err)
	}
}

// ---- Status ----------------------------------------------------------------

func TestStatus_Available(t *testing.T) {
	s := newTestStore(t)

	if got := s.Status("X1"); got != nil {
		t.Errorf("expected nil for unknown resource, got %+v", got)
	}
}

func TestStatus_Reserved(t *testing.T) {
	s := newTestStore(t)

	s.Reserve("X1", "owner-a", time.Hour)

	got := s.Status("X1")
	if got == nil {
		t.Fatal("expected reservation, got nil")
	}
	if got.Owner != "owner-a" {
		t.Errorf("owner = %q, want owner-a", got.Owner)
	}
}

func TestStatus_Expired(t *testing.T) {
	s := newTestStore(t)

	s.Reserve("X1", "owner-a", -time.Second)

	if got := s.Status("X1"); got != nil {
		t.Errorf("expected nil for expired reservation, got %+v", got)
	}
}

// ---- Purge -----------------------------------------------------------------

func TestPurge_RemovesExpired(t *testing.T) {
	s := newTestStore(t)

	s.Reserve("X1", "owner-a", -time.Second) // expired immediately
	s.Reserve("X2", "owner-b", time.Hour)    // still active

	if err := s.Purge(); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	s.mu.RLock()
	_, hasX1 := s.reservations["X1"]
	_, hasX2 := s.reservations["X2"]
	s.mu.RUnlock()

	if hasX1 {
		t.Error("X1 should have been purged")
	}
	if !hasX2 {
		t.Error("X2 should still be present")
	}
}

// ---- Persistence -----------------------------------------------------------

func TestPersistence_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	// First instance - create a reservation.
	s1, _ := New(stateFile)
	r, _ := s1.Reserve("X1", "owner-a", time.Hour)
	token := r.Token

	// Second instance - load from the same file.
	s2, err := New(stateFile)
	if err != nil {
		t.Fatalf("loading persisted state: %v", err)
	}

	got := s2.Status("X1")
	if got == nil {
		t.Fatal("reservation not found after reload")
	}
	if got.Token != token {
		t.Errorf("token mismatch: got %q, want %q", got.Token, token)
	}
	if got.Owner != "owner-a" {
		t.Errorf("owner = %q, want owner-a", got.Owner)
	}
}

func TestPersistence_ExpiredNotLoaded(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	s1, _ := New(stateFile)
	s1.Reserve("X1", "owner-a", -time.Second) // expired

	s2, _ := New(stateFile)
	if got := s2.Status("X1"); got != nil {
		t.Errorf("expired reservation should not be loaded, got %+v", got)
	}
}

func TestPersistence_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	s, _ := New(stateFile)
	s.Reserve("X1", "owner-a", time.Hour)

	// Temp file must not be left behind after a successful save.
	if _, err := os.Stat(stateFile + ".tmp"); !os.IsNotExist(err) {
		t.Error("tmp file still exists after save")
	}

	// State file must exist.
	if _, err := os.Stat(stateFile); err != nil {
		t.Errorf("state file missing: %v", err)
	}
}

// ---- Concurrency -----------------------------------------------------------

func TestConcurrency_ParallelReserves(t *testing.T) {
	s := newTestStore(t)

	results := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, err := s.Reserve("X1", "goroutine", time.Hour)
			results <- err
		}()
	}

	successCount := 0
	lockedCount := 0
	for i := 0; i < 10; i++ {
		err := <-results
		if err == nil {
			successCount++
		} else {
			var locked *ErrResourceLocked
			if errors.As(err, &locked) {
				lockedCount++
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}
	}

	if successCount != 1 {
		t.Errorf("expected exactly 1 successful reserve, got %d", successCount)
	}
	if lockedCount != 9 {
		t.Errorf("expected 9 locked errors, got %d", lockedCount)
	}
}
