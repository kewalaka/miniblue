package blob

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Azure lease status / state strings as returned in property responses.
const (
	LeaseStatusLocked   = "locked"
	LeaseStatusUnlocked = "unlocked"

	LeaseStateAvailable = "available"
	LeaseStateLeased    = "leased"
	LeaseStateExpired   = "expired"
	LeaseStateBreaking  = "breaking"
	LeaseStateBroken    = "broken"

	LeaseDurationInfinite = "infinite"
	LeaseDurationFixed    = "fixed"
)

// leaseRecord is the in-memory lease state for a single blob.
type leaseRecord struct {
	ID         string    // active lease id (GUID)
	Duration   int       // -1 == infinite
	AcquiredAt time.Time
	ExpiresAt  time.Time // zero for infinite
	BreakAt    time.Time // when state == breaking, the instant it transitions to broken
	State      string    // available|leased|expired|breaking|broken
}

// leaseManager tracks blob leases keyed by "account/container/blob".
type leaseManager struct {
	mu     sync.Mutex
	leases map[string]*leaseRecord
}

func newLeaseManager() *leaseManager {
	return &leaseManager{leases: make(map[string]*leaseRecord)}
}

func leaseKey(account, container, blob string) string {
	return account + "/" + container + "/" + blob
}

// resolve advances the lease state machine based on time. Caller must hold mu.
func (m *leaseManager) resolve(rec *leaseRecord, now time.Time) {
	if rec == nil {
		return
	}
	switch rec.State {
	case LeaseStateLeased:
		if rec.Duration > 0 && !rec.ExpiresAt.IsZero() && now.After(rec.ExpiresAt) {
			rec.State = LeaseStateExpired
		}
	case LeaseStateBreaking:
		if !rec.BreakAt.IsZero() && !now.Before(rec.BreakAt) {
			rec.State = LeaseStateBroken
		}
	}
}

// snapshot returns the externally visible status/state/duration for a blob,
// applying lazy expiry. Returns ("unlocked","available","") for no lease.
func (m *leaseManager) snapshot(account, container, blob string) (status, state, duration, leaseID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.leases[leaseKey(account, container, blob)]
	if !ok {
		return LeaseStatusUnlocked, LeaseStateAvailable, "", ""
	}
	m.resolve(rec, time.Now())
	switch rec.State {
	case LeaseStateLeased, LeaseStateBreaking:
		dur := LeaseDurationFixed
		if rec.Duration < 0 {
			dur = LeaseDurationInfinite
		}
		return LeaseStatusLocked, rec.State, dur, rec.ID
	case LeaseStateBroken, LeaseStateExpired, LeaseStateAvailable:
		return LeaseStatusUnlocked, rec.State, "", ""
	}
	return LeaseStatusUnlocked, LeaseStateAvailable, "", ""
}

// checkAccess validates a request lease-id against the current lease for
// PUT/DELETE-style operations. Returns (httpStatus, errCode, errMsg, ok).
//
//   - if there is no active lease (state is available/expired/broken): allow
//     regardless of supplied lease id.
//   - if the blob is leased and the request supplies a matching id: allow.
//   - if leased and no lease id supplied: 412 LeaseIdMissing.
//   - if leased and mismatched id: 412 LeaseIdMismatchWithBlobOperation.
func (m *leaseManager) checkAccess(account, container, blob, suppliedID string) (int, string, string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.leases[leaseKey(account, container, blob)]
	if !ok {
		return 0, "", "", true
	}
	m.resolve(rec, time.Now())
	if rec.State != LeaseStateLeased && rec.State != LeaseStateBreaking {
		return 0, "", "", true
	}
	if suppliedID == "" {
		return http.StatusPreconditionFailed, "LeaseIdMissing",
			"There is currently a lease on the blob and no lease ID was specified in the request.", false
	}
	if !strings.EqualFold(suppliedID, rec.ID) {
		return http.StatusPreconditionFailed, "LeaseIdMismatchWithBlobOperation",
			"The lease ID specified did not match the lease ID for the blob.", false
	}
	return 0, "", "", true
}

// handleLease is the entry point for PUT /{container}/{blob}?comp=lease.
func (h *Handler) handleLease(w http.ResponseWriter, r *http.Request) {
	account := chi.URLParam(r, "accountName")
	container := chi.URLParam(r, "containerName")
	blobName := blobNameParam(r)

	// Blob must exist.
	if _, ok := h.store.Get(h.blobKey(account, container, blobName)); !ok {
		writeBlobLeaseError(w, http.StatusNotFound, "BlobNotFound",
			"The specified blob does not exist.")
		return
	}

	action := strings.ToLower(r.Header.Get("x-ms-lease-action"))
	switch action {
	case "acquire":
		h.leaseAcquire(w, r, account, container, blobName)
	case "renew":
		h.leaseRenew(w, r, account, container, blobName)
	case "release":
		h.leaseRelease(w, r, account, container, blobName)
	case "break":
		h.leaseBreak(w, r, account, container, blobName)
	case "change":
		h.leaseChange(w, r, account, container, blobName)
	default:
		writeBlobLeaseError(w, http.StatusBadRequest, "InvalidHeaderValue",
			"The value for one of the HTTP headers is not in the correct format.")
	}
}

func (h *Handler) leaseAcquire(w http.ResponseWriter, r *http.Request, account, container, blob string) {
	durStr := r.Header.Get("x-ms-lease-duration")
	if durStr == "" {
		writeBlobLeaseError(w, http.StatusBadRequest, "MissingRequiredHeader",
			"An HTTP header that's mandatory for this request is not specified.")
		return
	}
	dur, err := strconv.Atoi(durStr)
	if err != nil || (dur != -1 && (dur < 15 || dur > 60)) {
		writeBlobLeaseError(w, http.StatusBadRequest, "InvalidHeaderValue",
			"The value for one of the HTTP headers is not in the correct format.")
		return
	}
	proposed := r.Header.Get("x-ms-proposed-lease-id")
	if proposed != "" {
		if _, perr := uuid.Parse(proposed); perr != nil {
			writeBlobLeaseError(w, http.StatusBadRequest, "InvalidHeaderValue",
				"The value for one of the HTTP headers is not in the correct format.")
			return
		}
	}

	mgr := h.leases
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	k := leaseKey(account, container, blob)
	rec := mgr.leases[k]
	mgr.resolve(rec, time.Now())

	// If currently leased: only allow if proposed id matches active lease id
	// (idempotent acquire). Breaking state never permits a new acquire.
	if rec != nil && (rec.State == LeaseStateLeased) {
		if proposed != "" && strings.EqualFold(proposed, rec.ID) {
			// idempotent: refresh duration & ID stays the same
			rec.Duration = dur
			rec.AcquiredAt = time.Now()
			if dur > 0 {
				rec.ExpiresAt = rec.AcquiredAt.Add(time.Duration(dur) * time.Second)
			} else {
				rec.ExpiresAt = time.Time{}
			}
			writeLeaseResponse(w, r, http.StatusCreated, rec.ID)
			return
		}
		writeBlobLeaseError(w, http.StatusConflict, "LeaseAlreadyPresent",
			"There is already a lease present.")
		return
	}
	if rec != nil && rec.State == LeaseStateBreaking {
		writeBlobLeaseError(w, http.StatusConflict, "LeaseAlreadyPresent",
			"There is already a lease present.")
		return
	}

	id := proposed
	if id == "" {
		id = uuid.New().String()
	}
	now := time.Now()
	newRec := &leaseRecord{
		ID:         id,
		Duration:   dur,
		AcquiredAt: now,
		State:      LeaseStateLeased,
	}
	if dur > 0 {
		newRec.ExpiresAt = now.Add(time.Duration(dur) * time.Second)
	}
	mgr.leases[k] = newRec
	writeLeaseResponse(w, r, http.StatusCreated, id)
}

func (h *Handler) leaseRenew(w http.ResponseWriter, r *http.Request, account, container, blob string) {
	id := r.Header.Get("x-ms-lease-id")
	if id == "" {
		writeBlobLeaseError(w, http.StatusBadRequest, "MissingRequiredHeader",
			"An HTTP header that's mandatory for this request is not specified.")
		return
	}
	mgr := h.leases
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	k := leaseKey(account, container, blob)
	rec := mgr.leases[k]
	mgr.resolve(rec, time.Now())
	if rec == nil || rec.State == LeaseStateAvailable || rec.State == LeaseStateBroken {
		writeBlobLeaseError(w, http.StatusConflict, "LeaseNotPresentWithLeaseOperation",
			"There is currently no lease on the blob.")
		return
	}
	if !strings.EqualFold(rec.ID, id) {
		writeBlobLeaseError(w, http.StatusConflict, "LeaseIdMismatchWithLeaseOperation",
			"The lease ID specified did not match the lease ID for the blob.")
		return
	}
	if rec.State == LeaseStateBreaking {
		writeBlobLeaseError(w, http.StatusConflict, "LeaseIsBreakingAndCannotBeRenewed",
			"The lease ID matched, but the lease is currently in breaking state and cannot be renewed.")
		return
	}
	// Reset duration window (state may be expired -> back to leased).
	now := time.Now()
	rec.AcquiredAt = now
	rec.State = LeaseStateLeased
	if rec.Duration > 0 {
		rec.ExpiresAt = now.Add(time.Duration(rec.Duration) * time.Second)
	} else {
		rec.ExpiresAt = time.Time{}
	}
	writeLeaseResponse(w, r, http.StatusOK, rec.ID)
}

func (h *Handler) leaseRelease(w http.ResponseWriter, r *http.Request, account, container, blob string) {
	id := r.Header.Get("x-ms-lease-id")
	if id == "" {
		writeBlobLeaseError(w, http.StatusBadRequest, "MissingRequiredHeader",
			"An HTTP header that's mandatory for this request is not specified.")
		return
	}
	mgr := h.leases
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	k := leaseKey(account, container, blob)
	rec := mgr.leases[k]
	mgr.resolve(rec, time.Now())
	if rec == nil || rec.State == LeaseStateAvailable || rec.State == LeaseStateBroken {
		writeBlobLeaseError(w, http.StatusConflict, "LeaseNotPresentWithLeaseOperation",
			"There is currently no lease on the blob.")
		return
	}
	if !strings.EqualFold(rec.ID, id) {
		writeBlobLeaseError(w, http.StatusConflict, "LeaseIdMismatchWithLeaseOperation",
			"The lease ID specified did not match the lease ID for the blob.")
		return
	}
	delete(mgr.leases, k)
	writeLeaseResponse(w, r, http.StatusOK, "")
}

func (h *Handler) leaseBreak(w http.ResponseWriter, r *http.Request, account, container, blob string) {
	mgr := h.leases
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	k := leaseKey(account, container, blob)
	rec := mgr.leases[k]
	mgr.resolve(rec, time.Now())
	if rec == nil || rec.State == LeaseStateAvailable || rec.State == LeaseStateBroken {
		writeBlobLeaseError(w, http.StatusConflict, "LeaseNotPresentWithLeaseOperation",
			"There is currently no lease on the blob.")
		return
	}

	now := time.Now()

	// Determine the requested break period (in seconds, 0..60). If omitted,
	// the remaining lease time is used (capped to the lease duration). For
	// infinite leases with no x-ms-lease-break-period it breaks immediately.
	bpStr := r.Header.Get("x-ms-lease-break-period")
	var period time.Duration
	periodSpecified := false
	if bpStr != "" {
		bp, err := strconv.Atoi(bpStr)
		if err != nil || bp < 0 || bp > 60 {
			writeBlobLeaseError(w, http.StatusBadRequest, "InvalidHeaderValue",
				"The value for one of the HTTP headers is not in the correct format.")
			return
		}
		period = time.Duration(bp) * time.Second
		periodSpecified = true
	} else if rec.State == LeaseStateLeased && rec.Duration > 0 && !rec.ExpiresAt.IsZero() {
		// remaining lease time
		if rec.ExpiresAt.After(now) {
			period = rec.ExpiresAt.Sub(now)
		}
	}

	// In 'breaking' state, a new break may shorten remaining time.
	if rec.State == LeaseStateBreaking {
		if periodSpecified {
			newBreak := now.Add(period)
			if newBreak.Before(rec.BreakAt) {
				rec.BreakAt = newBreak
			}
		}
	} else {
		if period <= 0 {
			rec.State = LeaseStateBroken
			rec.BreakAt = now
		} else {
			rec.State = LeaseStateBreaking
			rec.BreakAt = now.Add(period)
		}
	}

	remaining := 0
	if rec.State == LeaseStateBreaking {
		if d := time.Until(rec.BreakAt); d > 0 {
			remaining = int(d.Round(time.Second).Seconds())
		}
	}

	w.Header().Set("x-ms-lease-time", strconv.Itoa(remaining))
	writeLeaseResponse(w, r, http.StatusAccepted, "")
}

func (h *Handler) leaseChange(w http.ResponseWriter, r *http.Request, account, container, blob string) {
	cur := r.Header.Get("x-ms-lease-id")
	prop := r.Header.Get("x-ms-proposed-lease-id")
	if cur == "" || prop == "" {
		writeBlobLeaseError(w, http.StatusBadRequest, "MissingRequiredHeader",
			"An HTTP header that's mandatory for this request is not specified.")
		return
	}
	if _, err := uuid.Parse(prop); err != nil {
		writeBlobLeaseError(w, http.StatusBadRequest, "InvalidHeaderValue",
			"The value for one of the HTTP headers is not in the correct format.")
		return
	}
	mgr := h.leases
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	k := leaseKey(account, container, blob)
	rec := mgr.leases[k]
	mgr.resolve(rec, time.Now())
	if rec == nil || rec.State != LeaseStateLeased {
		writeBlobLeaseError(w, http.StatusConflict, "LeaseNotPresentWithLeaseOperation",
			"There is currently no lease on the blob.")
		return
	}
	if !strings.EqualFold(rec.ID, cur) {
		writeBlobLeaseError(w, http.StatusConflict, "LeaseIdMismatchWithLeaseOperation",
			"The lease ID specified did not match the lease ID for the blob.")
		return
	}
	rec.ID = prop
	writeLeaseResponse(w, r, http.StatusOK, prop)
}

// writeLeaseResponse sets the standard lease response headers and status.
// leaseID is included as the x-ms-lease-id header when non-empty.
func writeLeaseResponse(w http.ResponseWriter, r *http.Request, status int, leaseID string) {
	if v := r.Header.Get("X-Ms-Version"); v != "" {
		w.Header().Set("x-ms-version", v)
	}
	if id := r.Header.Get("x-ms-client-request-id"); id != "" {
		w.Header().Set("x-ms-client-request-id", id)
	}
	w.Header().Set("x-ms-request-id", uuid.New().String())
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	w.Header().Set("ETag", fmt.Sprintf("\"0x%X\"", time.Now().UnixNano()))
	if leaseID != "" {
		w.Header().Set("x-ms-lease-id", leaseID)
	}
	w.WriteHeader(status)
}

// writeBlobLeaseError emits an Azure Storage style XML error body, which is
// what the Azure SDK / Terraform azurerm backend parses to surface lease
// conflicts (e.g. for `terraform force-unlock`).
func writeBlobLeaseError(w http.ResponseWriter, status int, code, message string) {
	reqID := uuid.New().String()
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("x-ms-request-id", reqID)
	w.Header().Set("x-ms-error-code", code)
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w,
		`<?xml version="1.0" encoding="utf-8"?>`+
			`<Error><Code>%s</Code><Message>%s`+
			`RequestId:%s`+
			`Time:%s</Message></Error>`,
		code, message, reqID, time.Now().UTC().Format("2006-01-02T15:04:05.0000000Z"))
}
