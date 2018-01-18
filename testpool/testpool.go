package testpool

import (
	"io"
	"sync"
	"testing"
)

// TestPool represents a pool of items, usually all of the
// same type, that can be shared between tests.
type TestPool struct {
	// get is used to acquire items for the pool.
	get func(arg interface{}) (io.Closer, error)

	mu    sync.Mutex
	items map[interface{}]*poolItem
}

// PoolItem holds an item from a TestPool.
// Its methods may not be called concurrently,
// and it must be closed after use.
type PoolItem struct {
	*poolItem
}

type poolItem struct {
	// key holds the item's key so it can be deleted from the pool.
	key interface{}

	// pool holds the TestPool that the item came from.
	pool *TestPool

	// ready is closed when the value is made available
	// or when there's an error obtaining the value.
	ready chan struct{}

	// val holds the item value as returned from the get function.
	val io.Closer

	// err holds any error encountered trying to obtain the
	// value.
	err error

	// refCount holds the number of extant references to the item.
	refCount int
}

// NewTestPool returns a TestPool that acquires items by
// calling the given get function.
func NewTestPool(get func(arg interface{}) (io.Closer, error)) *TestPool {
	return &TestPool{
		get:   get,
		items: make(map[interface{}]*poolItem),
	}
}

// Get returns an item from the pool acquired by calling the get
// function with the given arg. This call does not block;
// the actual value can be retrieved later by calling Val.
func (p *TestPool) Get(arg interface{}) *PoolItem {
	p.mu.Lock()
	defer p.mu.Unlock()
	item := p.items[arg]
	if item == nil {
		item = &poolItem{
			key:   arg,
			pool:  p,
			ready: make(chan struct{}),
		}
		p.items[arg] = item
		go func() {
			item.val, item.err = p.get(arg)
			close(item.ready)
		}()
	}
	item.refCount++
	return &PoolItem{item}
}

// Val returns the actual value as returned by the
// get parameter to NewTestPool.
func (i *PoolItem) Val(t *testing.T) interface{} {
	t.Helper()
	<-i.ready
	if i.err != nil {
		t.Fatalf("cannot get item: %v", i.err)
	}
	return i.val
}

// Clone returns a copy of i that can be used
// independently, and must be closed after use.
func (i *PoolItem) Clone() *PoolItem {
	i.pool.mu.Lock()
	i.refCount++
	i.pool.mu.Unlock()
	return &PoolItem{i.poolItem}
}

// Close closes the item. Closing the last reference
// to an item with a given value will close the underlying
// value returned from the get function passed to NewTestPool.
func (i *PoolItem) Close() error {
	if i.poolItem == nil {
		panic("PoolItem closed twice")
	}
	i.pool.mu.Lock()
	if i.refCount--; i.refCount > 0 {
		i.pool.mu.Unlock()
		return nil
	}
	delete(i.pool.items, i.key)
	// Unlock the pool before closing so that we don't
	// block other potentially concurrent closes that might
	// be slow.
	i.pool.mu.Unlock()
	err := i.val.Close()
	i.poolItem = nil
	return err
}
