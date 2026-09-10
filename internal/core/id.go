package core

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	entropyMu sync.Mutex
	entropy   = ulid.Monotonic(rand.Reader, 0)
)

// NewID returns a new time-sortable ULID (26 chars, Crockford base32).
// Monotonic entropy keeps IDs created in the same millisecond ordered.
func NewID() string {
	entropyMu.Lock()
	defer entropyMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
}

// ParseID validates that s is a well-formed ULID.
func ParseID(s string) (string, error) {
	id, err := ulid.ParseStrict(s)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
