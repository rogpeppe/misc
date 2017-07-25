package fs

import (
	"context"
)

func Filter(ctx context.Context, src <-chan Item, accept func(Item) bool) <-chan Item {
	dst := make(chan Item)
	go runFilter(ctx, dst, src, accept)
	return dst
}

func runFilter(ctx context.Context, dst chan<- Item, src <-chan Item, accept func(Item) bool) {
	defer close(dst)
	reply := make(chan Answer)
	for it := range src {
		if it.Dir != nil && !accept(it) {
			it.Reply <- Next
			continue
		}
		dst <- it.WithReply(reply)
		it.Reply <- <-reply
	}
}
