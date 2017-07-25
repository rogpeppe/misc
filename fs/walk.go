package fs

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/errgo.v2/fmt/errors"
)

func Walk(ctx Context, path string, blockSize int) <-chan Item {
	c := make(chan Item)
	go runWalk(ctx, path, c, blockSize)
	return c
}

func runWalk(ctx Context, path string, c chan<- Item, blockSize int) {
	defer close(c)

	d, err := os.Stat(path)
	if err != nil {
		ctx.Report(err)
		return
	}
	if !d.IsDir() {
		ctx.Report(fmt.Errorf("%q is not a directory", path))
		return
	}
	reply := make(chan Answer)
	c <- Item{
		Dir:   d,
		Path:  path,
		Reply: reply,
	}
	if <-reply != Down {
		return
	}
	if !walkDir(ctx, path, c, blockSize) {
		return
	}
	c <- Item{Reply: reply}
	<-reply
}

func walkDir(ctx Context, path string, c chan<- Item, blockSize int) bool {
	dirs, err := ioutil.ReadDir(path)
	if err != nil {
		ctx.Report(err)
		return false
	}
	reply := make(chan Answer)
	for _, dir := range dirs {
		path := filepath.Join(path, dir.Name())
		if !dir.IsDir() {
			switch walkFile(ctx, dir, path, c, blockSize) {
			case Quit:
				return false
			case Next:
			case Skip:
				return true
			}
			continue
		}
		c <- Item{
			Dir:   dir,
			Path:  path,
			Reply: reply,
		}
		switch <-reply {
		case Quit:
			return false
		case Down:
			if !walkDir(ctx, path, c, blockSize) {
				return false
			}
			c <- Item{Reply: reply}
			if <-reply == Quit {
				return false
			}
		case Skip:
			return true
		}
	}
	return true
}

func walkFile(ctx Context, dir os.FileInfo, path string, c chan<- Item, blockSize int) Answer {
	f, err := os.Open(path)
	if err != nil {
		// When we can't open the file, report the error but still
		// continue to traverse the tree.
		ctx.Report(err)
		return Next
	}
	defer f.Close()
	reply := make(chan Answer)
	c <- Item{
		Dir:   dir,
		Path:  path,
		Reply: reply,
	}
	switch a := <-reply; a {
	case Quit, Skip, Next:
		return a
	case Down:
	}
	// Try to read exactly the amount of data reported by the FileInfo entry,
	// so that downstream modules can rely on the amount of data produced.
	size := dir.Size()
	for n := int64(0); n < size; {
		nr := blockSize
		if n+int64(blockSize) > size {
			nr = int(size - n)
		}
		// Note: we need to make a new data buffer
		// each time because we're passing it to another
		// goroutine.
		buf := make([]byte, nr)
		nr, err := f.Read(buf)
		if err != nil {
			ctx.Report(errors.Notef(err, nil, "error reading %q", path))
			return Quit
		}
		n += int64(nr)
		if nr < len(buf) {
			ctx.Report(errors.Newf("%q is shorter than expected (%d/%d)", path, n, size))
		}
		c <- Item{
			Data:  buf,
			Reply: reply,
		}
		switch <-reply {
		case Quit:
			return Quit
		case Skip:
			return Next
		}
	}
	c <- Item{
		Reply: reply,
	}
	if <-reply == Quit {
		return Quit
	}
	return Next
}
