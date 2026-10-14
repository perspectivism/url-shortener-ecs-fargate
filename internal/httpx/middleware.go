package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
)

type ctxKey int

const requestIDKey ctxKey = iota

// RequestIDFrom returns the request ID stored in ctx, or "".
func RequestIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

// WithRequestID returns a context carrying id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// validRequestID accepts CloudFront request IDs (base64url-ish, ~56 chars) and
// rejects anything that could inject into logs.
func validRequestID(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '=' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}

// RequestID uses CloudFront's X-Amz-Cf-Id as the request ID when present and
// well-formed, otherwise generates a UUIDv7. The ID is echoed as X-Request-Id.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Amz-Cf-Id")
		if !validRequestID(id) {
			id = newID()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), id)))
	})
}

func newID() string {
	if u, err := uuid.NewV7(); err == nil {
		return u.String()
	}
	return uuid.NewString()
}

// StatusRecorder captures the status code written by a handler.
type StatusRecorder struct {
	http.ResponseWriter
	Status int
}

// WriteHeader records the status.
func (s *StatusRecorder) WriteHeader(code int) {
	if s.Status == 0 {
		s.Status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *StatusRecorder) Write(b []byte) (int, error) {
	if s.Status == 0 {
		s.Status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Unwrap supports http.ResponseController.
func (s *StatusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Recover converts panics into 500 problems and logs them with a stack trace.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
						panic(v)
					}
					logger.ErrorContext(r.Context(), "panic", "request_id", RequestIDFrom(r.Context()), "panic", v, "stack", string(debug.Stack()))
					WriteProblem(w, r, http.StatusInternalServerError, CodeInternal, "")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// AccessLog logs one line per request: method, route pattern, status, latency.
// It deliberately logs the matched pattern (not the raw URL or query string).
// Extra attributes can be added by handlers via AddLogAttrs.
func AccessLog(logger *slog.Logger, now func() time.Time) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := now()
			rec := &StatusRecorder{ResponseWriter: w}
			attrs := &logAttrs{}
			r = r.WithContext(context.WithValue(r.Context(), logAttrsKey{}, attrs))
			next.ServeHTTP(rec, r)
			if rec.Status == 0 {
				rec.Status = http.StatusOK
			}
			if r.URL.Path == "/healthz" {
				return
			}
			args := []any{
				"request_id", RequestIDFrom(r.Context()),
				"method", r.Method,
				"route", r.Pattern,
				"status", rec.Status,
				"latency_ms", float64(now().Sub(start).Microseconds()) / 1000,
			}
			args = append(args, attrs.v...)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "request", slogAttrs(args)...)
		})
	}
}

type logAttrsKey struct{}

type logAttrs struct{ v []any }

// AddLogAttrs attaches key/value pairs to the request's access-log line.
func AddLogAttrs(ctx context.Context, kv ...any) {
	if a, ok := ctx.Value(logAttrsKey{}).(*logAttrs); ok {
		a.v = append(a.v, kv...)
	}
}

func slogAttrs(kv []any) []slog.Attr {
	out := make([]slog.Attr, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		k, _ := kv[i].(string)
		out = append(out, slog.Any(k, kv[i+1]))
	}
	return out
}

// Chain applies middleware so the first listed is the outermost.
func Chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}
