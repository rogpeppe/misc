package fs

import (
	"context"
)

func Entries(ctx context.Context, c <-chan Item) <-chan ItemInfo {
	ec := make(chan ItemInfo)
	go runEntries(ctx, c, ec)
	return ec
}

func runEntries(ctx context.Context, c <-chan Item, ec chan<- ItemInfo) {
	defer close(ec)
	depth := 0
	for item := range c {
		if item.Dir == nil {
			item.Reply <- Next
			if depth--; depth == 0 {
				return
			}
			continue
		}
		if item.Dir.IsDir() {
			depth++
			item.Reply <- Down
		} else {
			item.Reply <- Next
		}
		ec <- ItemInfo{
			Dir:   item.Dir,
			Path:  item.Path,
			Depth: depth,
		}
	}
}
