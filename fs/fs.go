package fs

import (
	"context"
	"os"
)

type FS <-chan Item

// Item represents an item of filesystem data.
// If Dir is non-nil, the item represents a directory
// entry. If Data is non-empty, the item represents
// a data block.
// When an item is received, the receiver is expected
// to send on the reply channel to indicate the
// next thing for the sender to do.
type Item struct {
	Dir os.FileInfo
	// Path holds the full path of the item, or the empty string
	// if Dir is nil.
	Path string

	Data  []byte
	Reply chan<- Answer
}

type ItemInfo struct {
	Path  string
	Dir   os.FileInfo
	Depth int
}

type Context interface {
	context.Context
	Report(err error)
}

type Answer int

const (
	// Quit requests the sender to stop sending data.
	Quit Answer = iota
	// Down requests the sender to show the contents
	// of the item. This should only be used to reply
	// to directory entry items.
	Down
	// Next requests that the sender send the next
	// item without descending into it.
	Next
	// Skip requests that the sender skip over
	// all remaining items in the current file or directory.
	Skip
)

// IsEnd reports whether the given item represents
// to end of a sequence of data or directory entries.
func (it Item) IsEnd() bool {
	return it.Data == nil && it.Dir == nil
}

// WithReply returns it with its reply channel set to r.
func (it Item) WithReply(r chan<- Answer) Item {
	return Item{
		Data:  it.Data,
		Dir:   it.Dir,
		Reply: r,
	}
}
