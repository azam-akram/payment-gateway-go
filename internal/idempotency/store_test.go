package idempotency

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStore_GetMiss(t *testing.T) {
	s := NewStore()

	_, ok := s.Get("unknown-key")

	assert.False(t, ok)
}

func TestStore_PutThenGet(t *testing.T) {
	s := NewStore()
	rec := Record{RequestHash: "hash-1", StatusCode: 201, Body: []byte(`{"id":"1"}`)}

	s.Put("key-1", rec)
	got, ok := s.Get("key-1")

	assert.True(t, ok)
	assert.Equal(t, rec, got)
}

func TestStore_Lock_SerializesSameKey(t *testing.T) {
	s := NewStore()
	var order []string
	var mu sync.Mutex

	var wg sync.WaitGroup
	wg.Add(2)

	unlockFirst := s.Lock("shared-key")
	go func() {
		defer wg.Done()
		// Blocks until the first holder unlocks.
		unlock := s.Lock("shared-key")
		defer unlock()
		mu.Lock()
		order = append(order, "second")
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		order = append(order, "first")
		mu.Unlock()
		unlockFirst()
	}()

	wg.Wait()

	assert.Equal(t, []string{"first", "second"}, order)
}

func TestStore_Lock_DifferentKeysDoNotBlockEachOther(t *testing.T) {
	s := NewStore()

	unlockA := s.Lock("key-a")
	defer unlockA()

	done := make(chan struct{})
	go func() {
		unlockB := s.Lock("key-b")
		defer unlockB()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("locking a different key should not block behind key-a's lock")
	}
}

func TestHashBody_SameInputSameHash(t *testing.T) {
	a := HashBody([]byte(`{"amount":100}`))
	b := HashBody([]byte(`{"amount":100}`))

	assert.Equal(t, a, b)
}

func TestHashBody_DifferentInputDifferentHash(t *testing.T) {
	a := HashBody([]byte(`{"amount":100}`))
	b := HashBody([]byte(`{"amount":200}`))

	assert.NotEqual(t, a, b)
}
