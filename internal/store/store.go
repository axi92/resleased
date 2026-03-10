package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"resleased/internal/model"
)

// ErrNotFound is returned when a token does not match any reservation.
var ErrNotFound = fmt.Errorf("reservation not found")

// ErrResourceLocked is returned when the requested resource is already reserved.
type ErrResourceLocked struct {
	ReservedUntil time.Time
	Remaining     time.Duration
	Owner         string
}

func (e *ErrResourceLocked) Error() string {
	return fmt.Sprintf("resource locked by %q for another %.0f seconds", e.Owner, e.Remaining.Seconds())
}

// persistedState is the on-disk JSON structure.
type persistedState struct {
	Reservations map[string]*model.Reservation `json:"reservations"`
}

// Store holds all active reservations and handles persistence.
type Store struct {
	mu           sync.RWMutex
	reservations map[string]*model.Reservation // keyed by resource ID
	tokenIndex   map[string]string             // token -> resource ID
	filePath     string
}

// New creates a Store and loads existing state from filePath if it exists.
func New(filePath string) (*Store, error) {
	s := &Store{
		reservations: make(map[string]*model.Reservation),
		tokenIndex:   make(map[string]string),
		filePath:     filePath,
	}
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading state: %w", err)
	}
	return s, nil
}

// Reserve attempts to reserve resourceID for owner for duration.
// Returns the new Reservation or an ErrResourceLocked if already taken.
func (s *Store) Reserve(resourceID, owner string, duration time.Duration) (*model.Reservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()

	if r, ok := s.reservations[resourceID]; ok && !r.IsExpired() {
		return nil, &ErrResourceLocked{
			ReservedUntil: r.ExpiresAt,
			Remaining:     r.Remaining(),
			Owner:         r.Owner,
		}
	}

	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("generating token: %w", err)
	}

	r := &model.Reservation{
		ResourceID: resourceID,
		Owner:      owner,
		Token:      token,
		ExpiresAt:  now.Add(duration),
		CreatedAt:  now,
	}

	s.reservations[resourceID] = r
	s.tokenIndex[token] = resourceID

	return r, s.save()
}

// Extend prolongs the reservation identified by token by duration.
// Returns the updated Reservation or ErrNotFound.
func (s *Store) Extend(token string, duration time.Duration) (*model.Reservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	resourceID, ok := s.tokenIndex[token]
	if !ok {
		return nil, ErrNotFound
	}

	r, ok := s.reservations[resourceID]
	if !ok || r.IsExpired() {
		delete(s.tokenIndex, token)
		return nil, ErrNotFound
	}

	r.ExpiresAt = r.ExpiresAt.Add(duration)
	return r, s.save()
}

// Release removes the reservation identified by token.
// Returns ErrNotFound if the token is unknown or expired.
func (s *Store) Release(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	resourceID, ok := s.tokenIndex[token]
	if !ok {
		return ErrNotFound
	}

	delete(s.tokenIndex, token)
	delete(s.reservations, resourceID)

	return s.save()
}

// Status returns the current reservation for resourceID, or nil if available.
func (s *Store) Status(resourceID string) *model.Reservation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.reservations[resourceID]
	if !ok || r.IsExpired() {
		return nil
	}
	return r
}

// Purge removes all expired reservations and persists the result.
func (s *Store) Purge() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, r := range s.reservations {
		if r.IsExpired() {
			delete(s.tokenIndex, r.Token)
			delete(s.reservations, id)
		}
	}
	return s.save()
}

// --- persistence ----------------------------------------------------------

func (s *Store) save() error {
	state := persistedState{Reservations: s.reservations}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	// write atomically via temp file
	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.filePath)
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	for id, r := range state.Reservations {
		if !r.IsExpired() {
			s.reservations[id] = r
			s.tokenIndex[r.Token] = id
		}
	}
	return nil
}

// --- helpers --------------------------------------------------------------

func generateToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
