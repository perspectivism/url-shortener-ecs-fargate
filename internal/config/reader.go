// Package config parses service configuration from environment variables into
// typed structs and validates it at startup. Error messages name the offending
// keys but never include their values.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Secret holds a sensitive configuration value. It never prints or logs its
// contents; call Reveal at the single point of use.
type Secret struct{ value string }

// NewSecret wraps a sensitive value.
func NewSecret(v string) Secret { return Secret{value: v} }

// Reveal returns the underlying value.
func (s Secret) Reveal() string { return s.value }

// IsSet reports whether a value is present.
func (s Secret) IsSet() bool { return s.value != "" }

// String implements fmt.Stringer without exposing the value.
func (s Secret) String() string { return "[redacted]" }

// GoString implements fmt.GoStringer without exposing the value.
func (s Secret) GoString() string { return "config.Secret([redacted])" }

// LogValue implements slog.LogValuer without exposing the value.
func (s Secret) LogValue() slog.Value { return slog.StringValue("[redacted]") }

// MarshalText keeps the value out of JSON/text encodings.
func (s Secret) MarshalText() ([]byte, error) { return []byte("[redacted]"), nil }

// LookupFunc matches os.LookupEnv.
type LookupFunc func(key string) (string, bool)

// Reader reads typed values and accumulates problems instead of failing on the
// first one, so a misconfigured service reports every bad key at once.
type Reader struct {
	lookup   LookupFunc
	problems []string
}

// NewReader returns a Reader backed by lookup (usually os.LookupEnv).
func NewReader(lookup LookupFunc) *Reader { return &Reader{lookup: lookup} }

func (r *Reader) raw(key string) (string, bool) {
	v, ok := r.lookup(key)
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}

// Problemf records a validation problem. Callers must not include values.
func (r *Reader) Problemf(format string, args ...any) {
	r.problems = append(r.problems, fmt.Sprintf(format, args...))
}

// String returns the value of key, or def when unset.
func (r *Reader) String(key, def string) string {
	if v, ok := r.raw(key); ok {
		return v
	}
	return def
}

// Required returns the value of key and records a problem when unset.
func (r *Reader) Required(key string) string {
	v, ok := r.raw(key)
	if !ok {
		r.Problemf("%s is required", key)
	}
	return v
}

// Secret returns key as a Secret; required controls whether absence is a problem.
func (r *Reader) Secret(key string, required bool) Secret {
	v, ok := r.raw(key)
	if !ok && required {
		r.Problemf("%s is required", key)
	}
	return NewSecret(v)
}

// Int returns key as an int within [minV, maxV], or def when unset.
func (r *Reader) Int(key string, def, minV, maxV int) int {
	v, ok := r.raw(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		r.Problemf("%s must be an integer", key)
		return def
	}
	if n < minV || n > maxV {
		r.Problemf("%s must be between %d and %d", key, minV, maxV)
		return def
	}
	return n
}

// Duration returns key parsed by time.ParseDuration (must be > 0), or def.
func (r *Reader) Duration(key string, def time.Duration) time.Duration {
	v, ok := r.raw(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		r.Problemf("%s must be a positive duration (e.g. 1s, 500ms)", key)
		return def
	}
	return d
}

// Bool returns key parsed by strconv.ParseBool, or def.
func (r *Reader) Bool(key string, def bool) bool {
	v, ok := r.raw(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		r.Problemf("%s must be a boolean", key)
		return def
	}
	return b
}

// OneOf returns key when it is one of allowed, or def when unset.
func (r *Reader) OneOf(key, def string, allowed ...string) string {
	v, ok := r.raw(key)
	if !ok {
		return def
	}
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	r.Problemf("%s must be one of: %s", key, strings.Join(allowed, ", "))
	return def
}

// List returns key split on commas (empty items dropped), or nil.
func (r *Reader) List(key string) []string {
	v, ok := r.raw(key)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// Err returns all accumulated problems as one error, or nil.
func (r *Reader) Err() error {
	if len(r.problems) == 0 {
		return nil
	}
	return errors.New("invalid configuration: " + strings.Join(r.problems, "; "))
}
