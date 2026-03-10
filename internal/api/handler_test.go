package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"resleased/internal/store"
)

// newTestHandler creates a Handler backed by a temp-file store.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	f := filepath.Join(t.TempDir(), "state.json")
	s, err := store.New(f)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	return New(s)
}

// do fires a request against the handler and returns the response.
func do(t *testing.T, h *Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encoding request body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// decodeBody decodes the response JSON into a map.
func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&m); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return m
}

// ---- POST /api/v1/reserve --------------------------------------------------

func TestReserveEndpoint_Success(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/api/v1/reserve", map[string]any{
		"resource_id": "X1",
		"owner":       "ci-job-1",
		"duration":    "2h",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	body := decodeBody(t, rec)
	if body["token"] == "" {
		t.Error("expected non-empty token in response")
	}
	if body["expires_at"] == "" {
		t.Error("expected expires_at in response")
	}
}

func TestReserveEndpoint_Locked(t *testing.T) {
	h := newTestHandler(t)

	do(t, h, http.MethodPost, "/api/v1/reserve", map[string]any{
		"resource_id": "X1", "owner": "job-1", "duration": "2h",
	})

	rec := do(t, h, http.MethodPost, "/api/v1/reserve", map[string]any{
		"resource_id": "X1", "owner": "job-2", "duration": "1h",
	})

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	body := decodeBody(t, rec)
	if body["error"] != "resource locked" {
		t.Errorf("error = %q, want 'resource locked'", body["error"])
	}
	if body["owner"] != "job-1" {
		t.Errorf("owner = %q, want job-1", body["owner"])
	}
	if body["remaining_seconds"] == nil {
		t.Error("expected remaining_seconds in 503 response")
	}
	if body["reserved_until"] == nil {
		t.Error("expected reserved_until in 503 response")
	}
}

func TestReserveEndpoint_MissingFields(t *testing.T) {
	h := newTestHandler(t)

	cases := []map[string]any{
		{"owner": "job-1", "duration": "1h"},    // missing resource_id
		{"resource_id": "X1", "duration": "1h"}, // missing owner
		{"resource_id": "X1", "owner": "job-1"}, // missing duration
	}
	for _, body := range cases {
		rec := do(t, h, http.MethodPost, "/api/v1/reserve", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body=%v: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestReserveEndpoint_InvalidDuration(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/api/v1/reserve", map[string]any{
		"resource_id": "X1", "owner": "job-1", "duration": "banana",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestReserveEndpoint_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reserve", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// ---- POST /api/v1/extend ---------------------------------------------------

func TestExtendEndpoint_Success(t *testing.T) {
	h := newTestHandler(t)

	res := do(t, h, http.MethodPost, "/api/v1/reserve", map[string]any{
		"resource_id": "X1", "owner": "job-1", "duration": "1h",
	})

	// decode once, reuse the map
	resBody := decodeBody(t, res)
	token := resBody["token"].(string)
	firstExpiry := resBody["expires_at"]

	rec := do(t, h, http.MethodPost, "/api/v1/extend", map[string]any{
		"token": token, "duration": "30m",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}

	body := decodeBody(t, rec)
	newExpiry := body["expires_at"]
	if newExpiry == firstExpiry {
		t.Error("expires_at did not change after extend")
	}
}

func TestExtendEndpoint_UnknownToken(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/api/v1/extend", map[string]any{
		"token": "ghost-token", "duration": "1h",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestExtendEndpoint_MissingFields(t *testing.T) {
	h := newTestHandler(t)

	cases := []map[string]any{
		{"duration": "1h"}, // missing token
		{"token": "abc"},   // missing duration
	}
	for _, body := range cases {
		rec := do(t, h, http.MethodPost, "/api/v1/extend", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body=%v: status = %d, want 400", body, rec.Code)
		}
	}
}

// ---- DELETE /api/v1/release ------------------------------------------------

func TestReleaseEndpoint_Success(t *testing.T) {
	h := newTestHandler(t)

	res := do(t, h, http.MethodPost, "/api/v1/reserve", map[string]any{
		"resource_id": "X1", "owner": "job-1", "duration": "1h",
	})
	token := decodeBody(t, res)["token"].(string)

	rec := do(t, h, http.MethodDelete, "/api/v1/release", map[string]any{
		"token": token,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	body := decodeBody(t, rec)
	if body["released"] != true {
		t.Errorf("released = %v, want true", body["released"])
	}
}

func TestReleaseEndpoint_UnknownToken(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodDelete, "/api/v1/release", map[string]any{
		"token": "no-such-token",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestReleaseEndpoint_MissingToken(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodDelete, "/api/v1/release", map[string]any{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestReleaseEndpoint_AllowsReReservation(t *testing.T) {
	h := newTestHandler(t)

	res := do(t, h, http.MethodPost, "/api/v1/reserve", map[string]any{
		"resource_id": "X1", "owner": "job-1", "duration": "1h",
	})
	token := decodeBody(t, res)["token"].(string)
	do(t, h, http.MethodDelete, "/api/v1/release", map[string]any{"token": token})

	rec := do(t, h, http.MethodPost, "/api/v1/reserve", map[string]any{
		"resource_id": "X1", "owner": "job-2", "duration": "1h",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("re-reservation after release: status = %d, want 200", rec.Code)
	}
}

// ---- GET /api/v1/status ----------------------------------------------------

func TestStatusEndpoint_Available(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/api/v1/status/X1", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decodeBody(t, rec)
	if body["available"] != true {
		t.Errorf("available = %v, want true", body["available"])
	}
}

func TestStatusEndpoint_Reserved(t *testing.T) {
	h := newTestHandler(t)

	do(t, h, http.MethodPost, "/api/v1/reserve", map[string]any{
		"resource_id": "X1", "owner": "job-1", "duration": "2h",
	})

	rec := do(t, h, http.MethodGet, "/api/v1/status/X1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decodeBody(t, rec)
	if body["available"] != false {
		t.Errorf("available = %v, want false", body["available"])
	}
	if body["owner"] != "job-1" {
		t.Errorf("owner = %q, want job-1", body["owner"])
	}
	if body["remaining_seconds"] == nil {
		t.Error("expected remaining_seconds in response")
	}
}

func TestStatusEndpoint_MissingResourceID(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/api/v1/status/", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// ---- Full lifecycle --------------------------------------------------------

func TestFullLifecycle(t *testing.T) {
	h := newTestHandler(t)

	// 1. Reserve
	res := do(t, h, http.MethodPost, "/api/v1/reserve", map[string]any{
		"resource_id": "board-1", "owner": "ci-42", "duration": "1h",
	})
	if res.Code != http.StatusOK {
		t.Fatalf("reserve: %d", res.Code)
	}
	token := decodeBody(t, res)["token"].(string)

	// 2. Second caller gets 503
	rec2 := do(t, h, http.MethodPost, "/api/v1/reserve", map[string]any{
		"resource_id": "board-1", "owner": "ci-99", "duration": "1h",
	})
	if rec2.Code != http.StatusServiceUnavailable {
		t.Fatalf("second reserve: want 503, got %d", rec2.Code)
	}

	// 3. Status shows locked
	st := do(t, h, http.MethodGet, "/api/v1/status/board-1", nil)
	if decodeBody(t, st)["available"] != false {
		t.Error("status should show unavailable")
	}

	// 4. Extend
	ext := do(t, h, http.MethodPost, "/api/v1/extend", map[string]any{
		"token": token, "duration": "30m",
	})
	if ext.Code != http.StatusOK {
		t.Fatalf("extend: %d", ext.Code)
	}

	// 5. Release
	rel := do(t, h, http.MethodDelete, "/api/v1/release", map[string]any{"token": token})
	if rel.Code != http.StatusOK {
		t.Fatalf("release: %d", rel.Code)
	}

	// 6. Status shows available again
	st2 := do(t, h, http.MethodGet, "/api/v1/status/board-1", nil)
	if decodeBody(t, st2)["available"] != true {
		t.Error("status should show available after release")
	}

	// 7. New caller can reserve
	rec3 := do(t, h, http.MethodPost, "/api/v1/reserve", map[string]any{
		"resource_id": "board-1", "owner": "ci-99", "duration": "1h",
	})
	if rec3.Code != http.StatusOK {
		t.Fatalf("re-reservation: want 200, got %d", rec3.Code)
	}
}

// ---- expires_at timestamp format -------------------------------------------

func TestReserveEndpoint_ExpiresAtFormat(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/api/v1/reserve", map[string]any{
		"resource_id": "X1", "owner": "job-1", "duration": "1h",
	})
	body := decodeBody(t, rec)
	expiresAt, ok := body["expires_at"].(string)
	if !ok || expiresAt == "" {
		t.Fatal("expires_at missing or not a string")
	}
	if _, err := time.Parse(time.RFC3339, expiresAt); err != nil {
		t.Errorf("expires_at %q is not RFC3339: %v", expiresAt, err)
	}
}
