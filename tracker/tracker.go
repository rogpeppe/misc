package tracker

import (
	"fmt"
	"sync"
	"os"
	"time"

	"github.com/rogpeppe/misc/runtime/debug"
)

var (
	trackedMu sync.Mutex
	tracked   = make(map[interface{}]*track)
)

type track struct {
	x       interface{}
	closed  bool
	allocBy []byte
}

func Alloc(x interface{}) {
	trackedMu.Lock()
	defer trackedMu.Unlock()
	if t, ok := tracked[x]; ok {
		panic(fmt.Errorf("double alloc of %#v (originally allocated by %s)", x, t.allocBy))
	}
	tracked[x] = &track{
		x:       x,
		allocBy: debug.Callers(1, 100),
	}
}

func Free(x interface{}, allowDouble bool) {
	trackedMu.Lock()
	defer trackedMu.Unlock()
	t, ok := tracked[x]
	if !ok {
		panic(fmt.Errorf("free of never-tracked item %#v", x))
	}
	if t.closed {
		if !allowDouble {
			panic(fmt.Errorf("double free of %#v (allocated by %s)", x, t.allocBy))
		}
		return
	}
	t.closed = true
}

func Check() bool {
	var remain []*track
	for i := 0; i < 10; i++ {
		remain = remaining()
		if len(remain) == 0 {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	printf := func(f string, a ...interface{}) {
		fmt.Fprintf(os.Stderr, f, a...)
	}
	printf("%d remaining unclosed items\n", len(remain))
	for _, t := range remain {
		printf("%#v %p %s\n", t.x, t.x, t.allocBy)
	}
	return false
}

func Realloc(x interface{}) {
	trackedMu.Lock()
	defer trackedMu.Unlock()
	t, ok := tracked[x]
	if !ok {
		panic(fmt.Errorf("realloc of unknown item %#v", x))
	}
	if t.closed {
		panic(fmt.Errorf("realloc of closed item %#v (allocated by %s)", x, t.allocBy))
	}
	t.allocBy = debug.Callers(1, 100)
}

func remaining() []*track {
	trackedMu.Lock()
	defer trackedMu.Unlock()
	var remain []*track
	for _, t := range tracked {
		if !t.closed {
			remain = append(remain, t)
		}
	}
	return remain
}
