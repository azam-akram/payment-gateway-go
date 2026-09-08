// Package idempotency lets the payments API guarantee that a request
// carrying an Idempotency-Key is only ever charged to the acquiring bank
// once, even if the client retries it (e.g. after a timeout) or two copies
// of it arrive concurrently. See decision.md D12.
package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// Record is the cached outcome of a request that completed successfully
// (Authorized/Declined) for a given idempotency key.
type Record struct {
	RequestHash string
	StatusCode  int
	Body        []byte
}

// Store serializes and caches responses per idempotency key, within a
// single process. It does not survive a restart and does not coordinate
// across replicas - the same limitation the in-memory payments repository
// already has (decision.md D6, §7).
type Store struct {
	mu      sync.Mutex
	locks   map[string]*sync.Mutex
	records map[string]Record
}

func NewStore() *Store {
	return &Store{
		locks:   make(map[string]*sync.Mutex),
		records: make(map[string]Record),
	}
}

// Lock serializes all requests sharing key so that, whether they arrive as
// an exact retry or a genuine race, only one of them ever reaches the bank.
// The caller must invoke the returned unlock exactly once.
func (s *Store) Lock(key string) (unlock func()) {
	s.mu.Lock()
	l, ok := s.locks[key]
	if !ok {
		l = &sync.Mutex{}
		s.locks[key] = l
	}
	s.mu.Unlock()

	l.Lock()
	return l.Unlock
}

// Get returns the cached record for key, if the request behind that key
// already ran to completion.
func (s *Store) Get(key string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[key]
	return rec, ok
}

// Put caches rec against key. Callers must hold the lock from Lock(key)
// while calling this, so a concurrent request can't observe a partially
// completed attempt.
func (s *Store) Put(key string, rec Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[key] = rec
}

// HashBody returns a content hash of a request body, used to tell a genuine
// retry (same key, same payload) apart from a key reused for a different
// payment (same key, different payload).
func HashBody(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
