// Package keygen generates short codes: a lightweight Key Generation Service
// that claims blocks of numeric IDs from a shared DynamoDB counter and encodes
// each ID with Sqids (ADR-0006).
//
// Sqids is obfuscation, not security: codes are decodable back to IDs. Links
// are not secrets, so unguessability is not a requirement.
package keygen

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/sqids/sqids-go"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/telemetry"
)

// MinLength is the minimum generated code length (SPEC §6).
const MinLength = 6

// BlockClaimer atomically reserves size IDs and returns the new counter value
// v; the caller owns the range (v-size, v].
type BlockClaimer interface {
	ClaimIDBlock(ctx context.Context, size int64) (int64, error)
}

// Metrics records counters.
type Metrics interface {
	Count(name string, v float64)
}

// Generator hands out codes from an in-memory ID range, claiming a new block
// lazily when the range is exhausted. IDs lost on restart are acceptable gaps.
type Generator struct {
	mu        sync.Mutex
	claimer   BlockClaimer
	blockSize int64
	next, end int64 // next ID to hand out; last ID in the current block
	enc       *sqids.Sqids
	skip      func(code string) bool
	metrics   Metrics
}

// New returns a Generator. skip reports codes that must never be issued (reserved words).
func New(claimer BlockClaimer, blockSize int, alphabet string, skip func(string) bool, metrics Metrics) (*Generator, error) {
	if blockSize < 1 {
		return nil, errors.New("keygen: block size must be positive")
	}
	enc, err := sqids.New(sqids.Options{Alphabet: alphabet, MinLength: MinLength})
	if err != nil {
		return nil, fmt.Errorf("keygen: sqids: %w", err)
	}
	if skip == nil {
		skip = func(string) bool { return false }
	}
	if metrics == nil {
		metrics = telemetry.Discard{}
	}
	return &Generator{claimer: claimer, blockSize: int64(blockSize), enc: enc, skip: skip, metrics: metrics}, nil
}

// NextID returns the next unused ID, claiming a block if needed.
func (g *Generator) NextID(ctx context.Context) (uint64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.next == 0 || g.next > g.end {
		v, err := g.claimer.ClaimIDBlock(ctx, g.blockSize)
		if err != nil {
			return 0, fmt.Errorf("keygen: claim block: %w", err)
		}
		if v < g.blockSize {
			return 0, fmt.Errorf("keygen: counter value %d is below block size %d", v, g.blockSize)
		}
		g.next, g.end = v-g.blockSize+1, v
		g.metrics.Count(telemetry.KeyBlocksAllocated, 1)
	}
	id := g.next
	g.next++
	if id < 1 {
		return 0, fmt.Errorf("keygen: invalid id %d", id)
	}
	return uint64(id), nil
}

// Encode returns the Sqids code for id.
func (g *Generator) Encode(id uint64) (string, error) {
	return g.enc.Encode([]uint64{id})
}

// NextCode returns the next code, skipping reserved ones.
func (g *Generator) NextCode(ctx context.Context) (string, error) {
	for {
		id, err := g.NextID(ctx)
		if err != nil {
			return "", err
		}
		code, err := g.Encode(id)
		if err != nil {
			return "", fmt.Errorf("keygen: encode: %w", err)
		}
		if !g.skip(code) {
			return code, nil
		}
	}
}
