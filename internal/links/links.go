// Package links holds the link domain: types, validation and normalization,
// the create/manage service (write path) and the cache-aside resolver (redirect
// path).
package links

import (
	"errors"
	"time"
)

// Status of a link.
type Status string

// Link statuses.
const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
	StatusDeleted  Status = "deleted"
)

// ScanStatus is the Safe Browsing verdict state of a link's destination.
type ScanStatus string

// Scan statuses.
const (
	ScanClean   ScanStatus = "clean"
	ScanPending ScanStatus = "pending"
	ScanFlagged ScanStatus = "flagged"
)

// DisabledReasonUnsafe is set when the nightly rescan flags a destination.
const DisabledReasonUnsafe = "unsafe_url"

// Link is a short link.
type Link struct {
	Code           string
	LongURL        string
	OwnerID        string
	Custom         bool
	Status         Status
	DisabledReason string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ExpiresAt      *time.Time
	ScanStatus     ScanStatus
	LastScannedAt  *time.Time
}

// Expired reports whether the link is past its expiry at now.
func (l Link) Expired(now time.Time) bool {
	return l.ExpiresAt != nil && !now.Before(*l.ExpiresAt)
}

// Summary is the list-view projection of a link (GSI attributes only).
type Summary struct {
	Code      string
	LongURL   string
	Status    Status
	CreatedAt time.Time
	ExpiresAt *time.Time
}

// Page is one page of an owner's links.
type Page struct {
	Items      []Summary
	NextCursor string
}

// Domain errors. Handlers map these to problem responses in one place.
var (
	ErrNotFound     = errors.New("link not found")
	ErrGone         = errors.New("link is expired, disabled or deleted")
	ErrAliasTaken   = errors.New("alias already taken")
	ErrInvalidURL   = errors.New("invalid url")
	ErrUnsafeURL    = errors.New("url flagged as unsafe")
	ErrInvalidInput = errors.New("invalid request")
	// ErrCodeTaken is returned by repositories when a conditional create fails.
	ErrCodeTaken = errors.New("code already exists")
	// ErrConflict is returned by repositories when an optimistic update loses a race.
	ErrConflict = errors.New("concurrent modification")
	// ErrInvalidCursor is returned for malformed or foreign pagination cursors.
	ErrInvalidCursor = errors.New("invalid cursor")
)

// ValidationError carries a client-safe detail for a domain error kind.
type ValidationError struct {
	Kind   error
	Detail string
}

func (e *ValidationError) Error() string { return e.Kind.Error() + ": " + e.Detail }

// Unwrap exposes the kind for errors.Is.
func (e *ValidationError) Unwrap() error { return e.Kind }

func invalid(kind error, detail string) error { return &ValidationError{Kind: kind, Detail: detail} }
