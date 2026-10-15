package links

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/scanner"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/telemetry"
)

// Repository persists links (implemented by store.Links).
type Repository interface {
	GetLink(ctx context.Context, code string) (Link, error)
	// CreateLink stores l only if its code is unused; else ErrCodeTaken.
	CreateLink(ctx context.Context, l Link) error
	// UpdateLink replaces l only if its stored updated_at equals prevUpdatedAt; else ErrConflict.
	UpdateLink(ctx context.Context, l Link, prevUpdatedAt time.Time) error
	ListLinks(ctx context.Context, owner string, limit int, cursor string) (Page, error)
}

// Invalidator removes a code's cache entry.
type Invalidator interface {
	DeleteLink(ctx context.Context, code string) error
}

// Scanner checks URLs against a threat list.
type Scanner interface {
	Check(ctx context.Context, urls []string) (map[string]scanner.Verdict, error)
}

// CodeSource yields fresh generated codes.
type CodeSource interface {
	NextCode(ctx context.Context) (string, error)
}

// Metrics records counters.
type Metrics interface {
	Count(name string, v float64)
}

// maxCodeAttempts bounds retries when a generated code collides (SPEC §6).
const maxCodeAttempts = 5

// ServiceDeps wires a Service.
type ServiceDeps struct {
	Repo         Repository
	Cache        Invalidator
	Scanner      Scanner
	Codes        CodeSource
	Metrics      Metrics
	Logger       *slog.Logger
	Now          func() time.Time
	BlockedHosts []string
	ScanTimeout  time.Duration
	CacheTimeout time.Duration
}

// Service implements link management (create, read, update, delete, admin).
type Service struct{ d ServiceDeps }

// NewService returns a Service, filling in defaults.
func NewService(d ServiceDeps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Metrics == nil {
		d.Metrics = telemetry.Discard{}
	}
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.ScanTimeout == 0 {
		d.ScanTimeout = 300 * time.Millisecond
	}
	if d.CacheTimeout == 0 {
		d.CacheTimeout = 200 * time.Millisecond
	}
	return &Service{d: d}
}

func (s *Service) now() time.Time { return s.d.Now().UTC().Truncate(time.Millisecond) }

// CreateInput is a create-link request.
type CreateInput struct {
	URL       string
	Alias     string
	ExpiresAt *time.Time
}

// Create validates, scans and stores a new link (SPEC §4.2).
func (s *Service) Create(ctx context.Context, owner string, in CreateInput) (Link, error) {
	now := s.now()
	longURL, err := NormalizeURL(in.URL, s.d.BlockedHosts)
	if err != nil {
		return Link{}, err
	}
	if in.Alias != "" {
		if err := ValidateAlias(in.Alias); err != nil {
			return Link{}, err
		}
	}
	if in.ExpiresAt != nil {
		if err := ValidateExpiry(*in.ExpiresAt, now); err != nil {
			return Link{}, err
		}
		exp := in.ExpiresAt.UTC().Truncate(time.Second)
		in.ExpiresAt = &exp
	}
	scan, err := s.scan(ctx, longURL)
	if err != nil {
		return Link{}, err
	}

	l := Link{
		LongURL:    longURL,
		OwnerID:    owner,
		Custom:     in.Alias != "",
		Status:     StatusActive,
		CreatedAt:  now,
		UpdatedAt:  now,
		ExpiresAt:  in.ExpiresAt,
		ScanStatus: scan,
	}
	if scan == ScanClean {
		l.LastScannedAt = &now
	}

	if l.Custom {
		l.Code = in.Alias
		if err := s.d.Repo.CreateLink(ctx, l); err != nil {
			if errors.Is(err, ErrCodeTaken) {
				return Link{}, invalid(ErrAliasTaken, "alias is already in use")
			}
			return Link{}, fmt.Errorf("create link: %w", err)
		}
	} else if err := s.createGenerated(ctx, &l); err != nil {
		return Link{}, err
	}

	// Clear any negative-cache entry for this code (e.g. someone probed the alias).
	s.invalidate(ctx, l.Code)
	s.d.Metrics.Count(telemetry.LinksCreated, 1)
	return l, nil
}

func (s *Service) createGenerated(ctx context.Context, l *Link) error {
	for range maxCodeAttempts {
		code, err := s.d.Codes.NextCode(ctx)
		if err != nil {
			return fmt.Errorf("next code: %w", err)
		}
		l.Code = code
		err = s.d.Repo.CreateLink(ctx, *l)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrCodeTaken) {
			return fmt.Errorf("create link: %w", err)
		}
		// A custom alias already owns this code; skip the ID and try the next.
		s.d.Logger.WarnContext(ctx, "generated code collided; skipping id", "code", code)
	}
	return fmt.Errorf("create link: %d consecutive generated-code collisions", maxCodeAttempts)
}

// scan returns the scan status for u, or ErrUnsafeURL. Scanner outages and
// disabled scanning degrade to pending (rescanned nightly), never to failure.
func (s *Service) scan(ctx context.Context, u string) (ScanStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, s.d.ScanTimeout)
	defer cancel()
	verdicts, err := s.d.Scanner.Check(ctx, []string{u})
	switch {
	case errors.Is(err, scanner.ErrDisabled):
		s.d.Metrics.Count(telemetry.URLScanSkipped, 1)
		return ScanPending, nil
	case err != nil:
		s.d.Metrics.Count(telemetry.URLScanUnavailable, 1)
		s.d.Logger.WarnContext(ctx, "url scan unavailable; marking pending", "err", err)
		return ScanPending, nil
	case verdicts[u].Unsafe:
		s.d.Metrics.Count(telemetry.URLScanFlagged, 1)
		return "", invalid(ErrUnsafeURL, "url is flagged as unsafe by Google Safe Browsing")
	default:
		return ScanClean, nil
	}
}

// invalidate deletes the cache entry, retrying once (SPEC §4.3). It uses a
// context detached from the request so a client disconnect cannot skip it.
func (s *Service) invalidate(ctx context.Context, code string) {
	base := context.WithoutCancel(ctx)
	var err error
	for range 2 {
		cctx, cancel := context.WithTimeout(base, s.d.CacheTimeout)
		err = s.d.Cache.DeleteLink(cctx, code)
		cancel()
		if err == nil {
			return
		}
	}
	s.d.Metrics.Count(telemetry.CacheInvalidationFailed, 1)
	s.d.Logger.ErrorContext(ctx, "cache invalidation failed; staleness bounded by cache TTL", "code", code, "err", err)
}

// Get returns an owner's link. Other owners' and deleted links are ErrNotFound
// (never reveal existence).
func (s *Service) Get(ctx context.Context, owner, code string) (Link, error) {
	l, err := s.d.Repo.GetLink(ctx, code)
	if err != nil {
		return Link{}, err
	}
	if l.OwnerID != owner || l.Status == StatusDeleted {
		return Link{}, ErrNotFound
	}
	return l, nil
}

// List returns a page of the owner's links, newest first.
func (s *Service) List(ctx context.Context, owner string, limit int, cursor string) (Page, error) {
	return s.d.Repo.ListLinks(ctx, owner, limit, cursor)
}

// OptionalTime distinguishes "absent" (Set=false), "clear" (Set, nil) and "set".
type OptionalTime struct {
	Set   bool
	Value *time.Time
}

// UpdateInput is a partial update; nil/unset fields are unchanged.
type UpdateInput struct {
	URL       *string
	ExpiresAt OptionalTime
}

// Update changes an owner's link destination and/or expiry. A new URL is re-scanned.
func (s *Service) Update(ctx context.Context, owner, code string, in UpdateInput) (Link, error) {
	now := s.now()
	var (
		newURL  string
		newScan ScanStatus
	)
	if in.URL != nil {
		var err error
		if newURL, err = NormalizeURL(*in.URL, s.d.BlockedHosts); err != nil {
			return Link{}, err
		}
		if newScan, err = s.scan(ctx, newURL); err != nil {
			return Link{}, err
		}
	}
	if in.ExpiresAt.Set && in.ExpiresAt.Value != nil {
		if err := ValidateExpiry(*in.ExpiresAt.Value, now); err != nil {
			return Link{}, err
		}
	}
	return s.mutate(ctx, code, func(l *Link) error {
		if l.OwnerID != owner || l.Status == StatusDeleted {
			return ErrNotFound
		}
		if in.URL != nil {
			l.LongURL = newURL
			l.ScanStatus = newScan
			l.LastScannedAt = nil
			if newScan == ScanClean {
				l.LastScannedAt = &now
			}
		}
		if in.ExpiresAt.Set {
			l.ExpiresAt = nil
			if in.ExpiresAt.Value != nil {
				exp := in.ExpiresAt.Value.UTC().Truncate(time.Second)
				l.ExpiresAt = &exp
			}
		}
		return nil
	})
}

// Delete soft-deletes an owner's link; the redirect then returns 410. The item
// is kept so its code is never reissued.
func (s *Service) Delete(ctx context.Context, owner, code string) error {
	_, err := s.mutate(ctx, code, func(l *Link) error {
		if l.OwnerID != owner || l.Status == StatusDeleted {
			return ErrNotFound
		}
		l.Status = StatusDeleted
		return nil
	})
	return err
}

// Disable is the admin kill switch (SPEC F8).
func (s *Service) Disable(ctx context.Context, code, reason string) (Link, error) {
	return s.mutate(ctx, code, func(l *Link) error {
		if l.Status == StatusDeleted {
			return ErrNotFound
		}
		l.Status = StatusDisabled
		l.DisabledReason = reason
		return nil
	})
}

// Enable re-enables a disabled link unless it has expired (SPEC §9.2).
func (s *Service) Enable(ctx context.Context, code string) (Link, error) {
	now := s.now()
	return s.mutate(ctx, code, func(l *Link) error {
		if l.Status == StatusDeleted {
			return ErrNotFound
		}
		if l.Expired(now) {
			return invalid(ErrGone, "link has expired")
		}
		l.Status = StatusActive
		l.DisabledReason = ""
		return nil
	})
}

// mutate applies fn with optimistic concurrency (retrying lost races), then
// invalidates the cache (SPEC §4.3: database first, then DEL).
func (s *Service) mutate(ctx context.Context, code string, fn func(*Link) error) (Link, error) {
	const attempts = 3
	for range attempts {
		cur, err := s.d.Repo.GetLink(ctx, code)
		if err != nil {
			return Link{}, err
		}
		next := cur
		if err := fn(&next); err != nil {
			return Link{}, err
		}
		next.UpdatedAt = s.now()
		if !next.UpdatedAt.After(cur.UpdatedAt) {
			next.UpdatedAt = cur.UpdatedAt.Add(time.Millisecond)
		}
		err = s.d.Repo.UpdateLink(ctx, next, cur.UpdatedAt)
		if errors.Is(err, ErrConflict) {
			continue
		}
		if err != nil {
			return Link{}, fmt.Errorf("update link: %w", err)
		}
		s.invalidate(ctx, code)
		return next, nil
	}
	return Link{}, fmt.Errorf("update link: %w after %d attempts", ErrConflict, attempts)
}

// ApplyScan records a rescan verdict (nightly job). An unsafe destination
// disables an active link with reason unsafe_url; the cache entry is deleted
// by mutate. Deleted links are ErrNotFound. Returns whether the link was
// disabled by this call (false on re-runs: already disabled).
func (s *Service) ApplyScan(ctx context.Context, code string, unsafe bool) (bool, error) {
	now := s.now()
	disabled := false
	_, err := s.mutate(ctx, code, func(l *Link) error {
		if l.Status == StatusDeleted {
			return ErrNotFound
		}
		disabled = false
		l.LastScannedAt = &now
		if !unsafe {
			l.ScanStatus = ScanClean
			return nil
		}
		l.ScanStatus = ScanFlagged
		if l.Status == StatusActive {
			l.Status = StatusDisabled
			l.DisabledReason = DisabledReasonUnsafe
			disabled = true
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if disabled {
		s.d.Metrics.Count(telemetry.LinksDisabled, 1)
	}
	return disabled, nil
}
