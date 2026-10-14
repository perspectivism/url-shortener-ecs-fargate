// Package events defines the click event (SPEC §8.1) and the redirect
// service's best-effort SQS producer (ADR-0010).
package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Version is the current click event schema version.
const Version = 1

// ClickEvent is produced by the redirect service for every GET that redirects.
type ClickEvent struct {
	V            int       `json:"v"`
	EventID      string    `json:"event_id"`
	Code         string    `json:"code"`
	TS           time.Time `json:"ts"`
	Visitor      string    `json:"visitor"`
	ReferrerHost string    `json:"referrer_host"`
	Country      string    `json:"country"`
	UserAgent    string    `json:"user_agent"`
	IsBot        bool      `json:"is_bot"`
}

// ErrPoison marks a message that can never be processed (bad JSON or schema).
var ErrPoison = errors.New("poison message")

// Decode parses and validates a click event. Errors wrap ErrPoison.
func Decode(b []byte) (ClickEvent, error) {
	var e ClickEvent
	if err := json.Unmarshal(b, &e); err != nil {
		return ClickEvent{}, fmt.Errorf("%w: %w", ErrPoison, err)
	}
	switch {
	case e.V != Version:
		return ClickEvent{}, fmt.Errorf("%w: unsupported version %d", ErrPoison, e.V)
	case e.EventID == "" || len(e.EventID) > 64:
		return ClickEvent{}, fmt.Errorf("%w: invalid event_id", ErrPoison)
	case e.Code == "" || len(e.Code) > 32:
		return ClickEvent{}, fmt.Errorf("%w: invalid code", ErrPoison)
	case e.TS.IsZero():
		return ClickEvent{}, fmt.Errorf("%w: missing ts", ErrPoison)
	}
	return e, nil
}
