// Package testpool provides a way of sharing resources between
// independent parallel tests
package testpool

import (
	"io"
	"sync"
	"testing"
)

// Pool represents a pool of items, usually all of the
// same type, that can be shared between tests.
type Pool struct {
	// get is used to acquire items for the pool.
	get func(arg interface{}) (io.Closer, error)

	mu    sync.Mutex
	items map[interface{}]*poolItem
}

// Item holds an item from a Pool.
// Its methods may not be called concurrently,
// and it must be closed after use.
type Item struct {
	*poolItem
}

type poolItem struct {
	// key holds the item's key so it can be deleted from the pool.
	key interface{}

	// pool holds the Pool that the item came from.
	pool *Pool

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

// NewPool returns a Pool that acquires items by
// calling the given get function.
func NewPool(get func(arg interface{}) (io.Closer, error)) *Pool {
	return &Pool{
		get:   get,
		items: make(map[interface{}]*poolItem),
	}
}

// Get returns an item from the pool acquired by calling the get
// function with the given arg. This call does not block;
// the actual value can be retrieved later by calling Val.
func (p *Pool) Get(arg interface{}) *Item {
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
	return &Item{item}
}

// Val returns the actual value as returned by the
// get parameter to NewPool.
func (i *Item) Val(t *testing.T) interface{} {
	t.Helper()
	<-i.ready
	if i.err != nil {
		t.Fatalf("cannot get item: %v", i.err)
	}
	return i.val
}

// Clone returns a copy of i that can be used
// independently, and must be closed after use.
func (i *Item) Clone() *Item {
	i.pool.mu.Lock()
	i.refCount++
	i.pool.mu.Unlock()
	return &Item{i.poolItem}
}

// Close closes the item. Closing the last reference
// to an item with a given value will close the underlying
// value returned from the get function passed to NewPool.
func (i *Item) Close() error {
	if i.poolItem == nil {
		panic("Item closed twice")
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
