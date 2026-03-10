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

// ReserveRequest is the request body for reserving a resource.
type ReserveRequest struct {
	ResourceID string `json:"resource_id" example:"X1"`
	Owner      string `json:"owner"       example:"ci-job-42"`
	Duration   string `json:"duration"    example:"2h"`
}

// ReserveResponse is returned on a successful reservation.
type ReserveResponse struct {
	Token     string `json:"token"      example:"a3f9c1d2e4b5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3"`
	ExpiresAt string `json:"expires_at" example:"2026-03-10T15:04:05Z"`
}

// ExtendRequest is the request body for extending a reservation.
type ExtendRequest struct {
	Token    string `json:"token"    example:"a3f9c1d2e4b5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3"`
	Duration string `json:"duration" example:"1h"`
}

// ExtendResponse is returned on a successful extension.
type ExtendResponse struct {
	ExpiresAt string `json:"expires_at" example:"2026-03-10T16:04:05Z"`
}

// ReleaseRequest is the request body for releasing a reservation.
type ReleaseRequest struct {
	Token string `json:"token" example:"a3f9c1d2e4b5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3"`
}

// ReleaseResponse is returned on a successful release.
type ReleaseResponse struct {
	Released bool `json:"released" example:"true"`
}

// StatusAvailableResponse is returned when the resource is available.
type StatusAvailableResponse struct {
	Available bool `json:"available" example:"true"`
}

// StatusLockedResponse is returned when the resource is locked.
type StatusLockedResponse struct {
	Available        bool   `json:"available"         example:"false"`
	Owner            string `json:"owner"             example:"ci-job-42"`
	ReservedUntil    string `json:"reserved_until"    example:"2026-03-10T15:04:05Z"`
	RemainingSeconds int    `json:"remaining_seconds" example:"3547"`
}

// LockedResponse is returned with HTTP 503 when the resource is already reserved.
type LockedResponse struct {
	Error            string `json:"error"             example:"resource locked"`
	Owner            string `json:"owner"             example:"ci-job-17"`
	ReservedUntil    string `json:"reserved_until"    example:"2026-03-10T14:00:00Z"`
	RemainingSeconds int    `json:"remaining_seconds" example:"3547"`
}

// ErrorResponse is a generic error response.
type ErrorResponse struct {
	Error string `json:"error" example:"token is required"`
}

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
	h.mux.HandleFunc("GET /api/v1/openapi.json", h.openAPISpec)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// reserve godoc
//
//	@Summary		Reserve a resource
//	@Description	Attempts to acquire an exclusive lease on a resource for the specified duration.
//	@Description	If the resource is already reserved, returns HTTP 503 with the current owner and remaining time.
//	@Tags			reservations
//	@Accept			json
//	@Produce		json
//	@Param			request	body		ReserveRequest	true	"Reserve request"
//	@Success		200		{object}	ReserveResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		503		{object}	LockedResponse
//	@Router			/api/v1/reserve [post]
func (h *Handler) reserve(w http.ResponseWriter, r *http.Request) {
	var req ReserveRequest
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
			_ = json.NewEncoder(w).Encode(LockedResponse{
				Error:            "resource locked",
				Owner:            locked.Owner,
				ReservedUntil:    locked.ReservedUntil.Format(time.RFC3339),
				RemainingSeconds: int(locked.Remaining.Seconds()),
			})
			return
		}
		slog.Error("reserve", "err", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}

	jsonOK(w, ReserveResponse{
		Token:     reservation.Token,
		ExpiresAt: reservation.ExpiresAt.Format(time.RFC3339),
	})
}

// extend godoc
//
//	@Summary		Extend a reservation
//	@Description	Extends the expiry of an existing reservation by the specified duration.
//	@Tags			reservations
//	@Accept			json
//	@Produce		json
//	@Param			request	body		ExtendRequest	true	"Extend request"
//	@Success		200		{object}	ExtendResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Router			/api/v1/extend [post]
func (h *Handler) extend(w http.ResponseWriter, r *http.Request) {
	var req ExtendRequest
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

	jsonOK(w, ExtendResponse{
		ExpiresAt: reservation.ExpiresAt.Format(time.RFC3339),
	})
}

// release godoc
//
//	@Summary		Release a reservation
//	@Description	Releases an existing reservation immediately, making the resource available to others.
//	@Tags			reservations
//	@Accept			json
//	@Produce		json
//	@Param			request	body		ReleaseRequest	true	"Release request"
//	@Success		200		{object}	ReleaseResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Router			/api/v1/release [delete]
func (h *Handler) release(w http.ResponseWriter, r *http.Request) {
	var req ReleaseRequest
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

	jsonOK(w, ReleaseResponse{Released: true})
}

// status godoc
//
//	@Summary		Get resource status
//	@Description	Returns whether a resource is currently available or reserved.
//	@Tags			resources
//	@Produce		json
//	@Param			resource_id	path		string	true	"Resource ID"	example(X1)
//	@Success		200			{object}	StatusAvailableResponse
//	@Success		200			{object}	StatusLockedResponse
//	@Failure		400			{object}	ErrorResponse
//	@Router			/api/v1/status/{resource_id} [get]
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	resourceID := strings.TrimPrefix(r.URL.Path, "/api/v1/status/")
	if resourceID == "" {
		jsonError(w, http.StatusBadRequest, "resource_id is required in path")
		return
	}

	reservation := h.store.Status(resourceID)
	if reservation == nil {
		jsonOK(w, StatusAvailableResponse{Available: true})
		return
	}

	jsonOK(w, StatusLockedResponse{
		Available:        false,
		Owner:            reservation.Owner,
		ReservedUntil:    reservation.ExpiresAt.Format(time.RFC3339),
		RemainingSeconds: int(reservation.Remaining().Seconds()),
	})
}

// openAPISpec godoc
//
//	@Summary		OpenAPI specification
//	@Description	Returns the OpenAPI 3.0 specification for this API.
//	@Tags			meta
//	@Produce		json
//	@Success		200
//	@Router			/api/v1/openapi.json [get]
func (h *Handler) openAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(openapiSpec)
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
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
}
