// Package linkstest provides in-memory fakes of the links package's
// dependencies for unit and contract tests.
package linkstest

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links"
)

// Repo is an in-memory links.Repository with injectable failures.
type Repo struct {
	mu    sync.Mutex
	links map[string]links.Link
	// Err, when set, is returned by every method.
	Err error
	// ConflictOnce makes the next UpdateLink return links.ErrConflict.
	ConflictOnce bool
	Gets         int
}

// NewRepo returns an empty Repo.
func NewRepo() *Repo { return &Repo{links: map[string]links.Link{}} }

// Put stores l unconditionally (test setup).
func (r *Repo) Put(l links.Link) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.links[l.Code] = l
}

// GetLink implements links.Repository.
func (r *Repo) GetLink(_ context.Context, code string) (links.Link, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Gets++
	if r.Err != nil {
		return links.Link{}, r.Err
	}
	l, ok := r.links[code]
	if !ok {
		return links.Link{}, links.ErrNotFound
	}
	return l, nil
}

// CreateLink implements links.Repository.
func (r *Repo) CreateLink(_ context.Context, l links.Link) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	if _, ok := r.links[l.Code]; ok {
		return links.ErrCodeTaken
	}
	r.links[l.Code] = l
	return nil
}

// UpdateLink implements links.Repository.
func (r *Repo) UpdateLink(_ context.Context, l links.Link, prev time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	if r.ConflictOnce {
		r.ConflictOnce = false
		return links.ErrConflict
	}
	cur, ok := r.links[l.Code]
	if !ok || !cur.UpdatedAt.Equal(prev) {
		return links.ErrConflict
	}
	r.links[l.Code] = l
	return nil
}

// ListLinks implements links.Repository: newest first, cursor = offset.
func (r *Repo) ListLinks(_ context.Context, owner string, limit int, cursor string) (links.Page, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return links.Page{}, r.Err
	}
	offset := 0
	if cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return links.Page{}, links.ErrInvalidCursor
		}
		if offset, err = strconv.Atoi(string(b)); err != nil {
			return links.Page{}, links.ErrInvalidCursor
		}
	}
	var all []links.Link
	for _, l := range r.links {
		if l.OwnerID == owner && l.Status != links.StatusDeleted {
			all = append(all, l)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].Code > all[j].Code
		}
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	var p links.Page
	for i := offset; i < len(all) && len(p.Items) < limit; i++ {
		l := all[i]
		p.Items = append(p.Items, links.Summary{Code: l.Code, LongURL: l.LongURL, Status: l.Status, CreatedAt: l.CreatedAt, ExpiresAt: l.ExpiresAt})
	}
	if offset+limit < len(all) {
		p.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset + limit)))
	}
	return p, nil
}

// Cache is an in-memory links.ResolverCache and links.Invalidator. It models
// Valkey's semantics: fills are SET NX, and invalidation leaves a fence that
// blocks fills until ExpireFences (links.FenceTTL in Valkey).
type Cache struct {
	mu      sync.Mutex
	entries map[string]links.CacheEntry
	fenced  map[string]bool
	TTLs    map[string]time.Duration
	// GetErr / SetErr / DelErr inject failures.
	GetErr, SetErr, DelErr error
	Deletes                int
}

// NewCache returns an empty Cache.
func NewCache() *Cache {
	return &Cache{entries: map[string]links.CacheEntry{}, fenced: map[string]bool{}, TTLs: map[string]time.Duration{}}
}

// GetLink implements links.ResolverCache.
func (c *Cache) GetLink(_ context.Context, code string) (links.CacheEntry, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.GetErr != nil {
		return links.CacheEntry{}, false, c.GetErr
	}
	e, ok := c.entries[code]
	return e, ok, nil
}

// FillLink implements links.ResolverCache (SET NX).
func (c *Cache) FillLink(_ context.Context, code string, e links.CacheEntry, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.SetErr != nil {
		return c.SetErr
	}
	if _, ok := c.entries[code]; ok || c.fenced[code] {
		return nil
	}
	c.entries[code] = e
	c.TTLs[code] = ttl
	return nil
}

// Put stores an entry unconditionally (test setup).
func (c *Cache) Put(code string, e links.CacheEntry, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[code] = e
	c.TTLs[code] = ttl
}

// InvalidateLink implements links.Invalidator.
func (c *Cache) InvalidateLink(_ context.Context, code string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Deletes++
	if c.DelErr != nil {
		return c.DelErr
	}
	delete(c.entries, code)
	c.fenced[code] = true
	return nil
}

// ExpireFences ends every invalidation fence (FenceTTL elapsed).
func (c *Cache) ExpireFences() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.fenced)
}

// Entry returns the cached entry for code.
func (c *Cache) Entry(code string) (links.CacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[code]
	return e, ok
}

// Codes yields codes from a fixed list, then errors.
type Codes struct {
	mu   sync.Mutex
	List []string
}

// NextCode implements links.CodeSource.
func (c *Codes) NextCode(context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.List) == 0 {
		return "", errors.New("codes exhausted")
	}
	code := c.List[0]
	c.List = c.List[1:]
	return code, nil
}

// SeqCodes yields "code000001", "code000002", ... forever.
type SeqCodes struct {
	mu sync.Mutex
	n  int
}

// NextCode implements links.CodeSource.
func (c *SeqCodes) NextCode(context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return fmt.Sprintf("code%06d", c.n), nil
}

// Counter records metric counts.
type Counter struct {
	mu sync.Mutex
	m  map[string]float64
}

// Count implements the Metrics interfaces.
func (c *Counter) Count(name string, v float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]float64{}
	}
	c.m[name] += v
}

// Observe implements telemetry.Recorder.
func (c *Counter) Observe(string, float64) {}

// Get returns the count for name.
func (c *Counter) Get(name string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[name]
}
