package model

import "time"

// Reservation represents an active resource lease.
type Reservation struct {
	ResourceID string    `json:"resource_id"`
	Owner      string    `json:"owner"`
	Token      string    `json:"token"`
	ExpiresAt  time.Time `json:"expires_at"`  // stored as RFC3339 timestamp
	CreatedAt  time.Time `json:"created_at"`
}

// IsExpired returns true if the reservation has passed its expiry time.
func (r *Reservation) IsExpired() bool {
	return time.Now().UTC().After(r.ExpiresAt)
}

// Remaining returns how long until the reservation expires.
func (r *Reservation) Remaining() time.Duration {
	return time.Until(r.ExpiresAt)
}
