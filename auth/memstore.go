package auth

import (
	"log"
	"sync"
	"time"

	"golang.org/x/net/context"
)

type memMultiOpStore struct {
	mu  sync.Mutex
	ops map[string][]Op
}

func NewMemMultiOpStore() MultiOpStore {
	return &memMultiOpStore{
		ops: make(map[string][]Op),
	}
}

// PutMultiOp implements MultiOpStore.PutMultiOp.
func (s *memMultiOpStore) PutMultiOp(_ context.Context, key string, ops []Op, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	log.Printf("multiop store %s = %#v", key, ops)
	s.ops[key] = append([]Op(nil), ops...)
	return nil
}

// GetMultiOp implements MultiOpStore.GetMultiOp.
func (s *memMultiOpStore) GetMultiOp(ctxt context.Context, key string) ([]Op, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ops, ok := s.ops[key]
	if !ok {
		return nil, ErrNotFound
	}
	log.Printf("multiop get %s = %#v", key, ops)
	return ops, nil
}
