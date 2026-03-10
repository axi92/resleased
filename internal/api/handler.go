package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"resleased/internal/store"
)

// Handler wires the HTTP routes for the resource lease API.
type Handler struct {
	store *store.Store
	mux   *http.ServeMux
}

func New(s *store.Store) *Handler {
	h := &Handler{store: s, mux: http.NewServeMux()}
	h.mux.HandleFunc("POST /api/v1/reserve", h.reserve)
	h.mux.HandleFunc("POST /api/v1/extend", h.extend)
	h.mux.HandleFunc("DELETE /api/v1/release", h.release)
	h.mux.HandleFunc("GET /api/v1/status/", h.status)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// --- POST /api/v1/reserve -------------------------------------------------
//
// Request:
//
//	{ "resource_id": "X1", "owner": "ci-job-42", "duration": "2h" }
//
// Response 200:
//
//	{ "token": "...", "expires_at": "2026-03-10T15:04:05Z" }
//
// Response 503:
//
//	{ "error": "resource locked", "owner": "...", "reserved_until": "...", "remaining_seconds": 7200 }
func (h *Handler) reserve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ResourceID string `json:"resource_id"`
		Owner      string `json:"owner"`
		Duration   string `json:"duration"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.ResourceID == "" || req.Owner == "" || req.Duration == "" {
		jsonError(w, http.StatusBadRequest, "resource_id, owner and duration are required")
		return
	}

	dur, err := time.ParseDuration(req.Duration)
	if err != nil || dur <= 0 {
		jsonError(w, http.StatusBadRequest, "invalid duration (use Go format: 2h, 30m, 1h30m)")
		return
	}

	reservation, err := h.store.Reserve(req.ResourceID, req.Owner, dur)
	if err != nil {
		var locked *store.ErrResourceLocked
		if errors.As(err, &locked) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":            "resource locked",
				"owner":            locked.Owner,
				"reserved_until":   locked.ReservedUntil.Format(time.RFC3339),
				"remaining_seconds": int(locked.Remaining.Seconds()),
			})
			return
		}
		slog.Error("reserve", "err", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}

	jsonOK(w, map[string]any{
		"token":      reservation.Token,
		"expires_at": reservation.ExpiresAt.Format(time.RFC3339),
	})
}

// --- POST /api/v1/extend --------------------------------------------------
//
// Request:
//
//	{ "token": "...", "duration": "1h" }
//
// Response 200:
//
//	{ "expires_at": "..." }
func (h *Handler) extend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Duration string `json:"duration"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Token == "" || req.Duration == "" {
		jsonError(w, http.StatusBadRequest, "token and duration are required")
		return
	}

	dur, err := time.ParseDuration(req.Duration)
	if err != nil || dur <= 0 {
		jsonError(w, http.StatusBadRequest, "invalid duration")
		return
	}

	reservation, err := h.store.Extend(req.Token, dur)
	if errors.Is(err, store.ErrNotFound) {
		jsonError(w, http.StatusNotFound, "token not found or reservation expired")
		return
	}
	if err != nil {
		slog.Error("extend", "err", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}

	jsonOK(w, map[string]any{
		"expires_at": reservation.ExpiresAt.Format(time.RFC3339),
	})
}

// --- DELETE /api/v1/release -----------------------------------------------
//
// Request:
//
//	{ "token": "..." }
//
// Response 200:
//
//	{ "released": true }
func (h *Handler) release(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Token == "" {
		jsonError(w, http.StatusBadRequest, "token is required")
		return
	}

	err := h.store.Release(req.Token)
	if errors.Is(err, store.ErrNotFound) {
		jsonError(w, http.StatusNotFound, "token not found or reservation already expired")
		return
	}
	if err != nil {
		slog.Error("release", "err", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}

	jsonOK(w, map[string]any{"released": true})
}

// --- GET /api/v1/status/{resource_id} ------------------------------------
//
// Response 200 (available):
//
//	{ "available": true }
//
// Response 200 (locked):
//
//	{ "available": false, "owner": "...", "reserved_until": "...", "remaining_seconds": 3600 }
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	resourceID := strings.TrimPrefix(r.URL.Path, "/api/v1/status/")
	if resourceID == "" {
		jsonError(w, http.StatusBadRequest, "resource_id is required in path")
		return
	}

	reservation := h.store.Status(resourceID)
	if reservation == nil {
		jsonOK(w, map[string]any{"available": true})
		return
	}

	jsonOK(w, map[string]any{
		"available":        false,
		"owner":            reservation.Owner,
		"reserved_until":   reservation.ExpiresAt.Format(time.RFC3339),
		"remaining_seconds": int(reservation.Remaining().Seconds()),
	})
}

// --- helpers --------------------------------------------------------------

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
