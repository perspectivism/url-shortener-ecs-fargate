// Package contract validates HTTP requests and responses against
// api/openapi.yaml. It is used by the handler contract tests and by the
// integration tests against the running services.
package contract

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	verrors "github.com/pb33f/libopenapi-validator/errors"
)

// Validator checks traffic against the OpenAPI document.
type Validator struct {
	mu sync.Mutex
	v  validator.Validator
}

// Parse builds a Validator from an OpenAPI document and checks the document itself.
func Parse(spec []byte) (*Validator, error) {
	doc, err := libopenapi.NewDocument(spec)
	if err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	v, errs := validator.NewValidator(doc)
	if len(errs) > 0 {
		return nil, fmt.Errorf("build validator: %v", errs)
	}
	if ok, verrs := v.ValidateDocument(); !ok {
		return nil, fmt.Errorf("spec is not a valid OpenAPI document: %s", format(verrs))
	}
	return &Validator{v: v}, nil
}

// Request validates a request (the body is buffered and restored).
func (c *Validator) Request(r *http.Request) error {
	if err := buffer(&r.Body); err != nil {
		return err
	}
	c.mu.Lock()
	ok, errs := c.v.ValidateHttpRequestSync(r)
	c.mu.Unlock()
	if err := buffer(&r.Body); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("request %s %s violates the contract: %s", r.Method, r.URL.Path, format(errs))
	}
	return nil
}

// Response validates resp for the operation matched by method and path of r.
// The response body is buffered and restored so callers can still read it.
func (c *Validator) Response(r *http.Request, resp *http.Response) error {
	if err := buffer(&resp.Body); err != nil {
		return err
	}
	match := r.Clone(r.Context()) // only method, path and headers are used for matching
	match.Body = http.NoBody
	c.mu.Lock()
	ok, errs := c.v.ValidateHttpResponse(match, resp)
	c.mu.Unlock()
	if err := buffer(&resp.Body); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("response %d to %s %s violates the contract: %s", resp.StatusCode, r.Method, r.URL.Path, format(errs))
	}
	return nil
}

// buffer replaces *body with a re-readable copy (nil stays an empty body).
func buffer(body *io.ReadCloser) error {
	if *body == nil {
		*body = io.NopCloser(bytes.NewReader(nil))
		return nil
	}
	b, err := io.ReadAll(*body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	_ = (*body).Close()
	*body = io.NopCloser(bytes.NewReader(b))
	return nil
}

func format(errs []*verrors.ValidationError) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		msg := e.Message
		if e.Reason != "" {
			msg += " (" + e.Reason + ")"
		}
		for _, se := range e.SchemaValidationErrors {
			msg += "; " + se.Reason + " at " + se.FieldPath
		}
		parts = append(parts, msg)
	}
	return strings.Join(parts, " | ")
}
