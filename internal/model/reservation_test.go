package model

import (
	"testing"
	"time"
)

func TestIsExpired_NotExpired(t *testing.T) {
	r := Reservation{ExpiresAt: time.Now().Add(time.Hour)}
	if r.IsExpired() {
		t.Error("expected not expired")
	}
}

func TestIsExpired_Expired(t *testing.T) {
	r := Reservation{ExpiresAt: time.Now().Add(-time.Second)}
	if !r.IsExpired() {
		t.Error("expected expired")
	}
}

func TestRemaining_Positive(t *testing.T) {
	r := Reservation{ExpiresAt: time.Now().Add(time.Hour)}
	if r.Remaining() <= 0 {
		t.Errorf("expected positive remaining, got %v", r.Remaining())
	}
}

func TestRemaining_Expired(t *testing.T) {
	r := Reservation{ExpiresAt: time.Now().Add(-time.Second)}
	if r.Remaining() >= 0 {
		t.Errorf("expected negative remaining for expired reservation, got %v", r.Remaining())
	}
}
