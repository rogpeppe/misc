package main
import (
	"fmt"
	"bytes"
	"github.com/rogpeppe/misc/bug/bug2"
)

type X struct {
	*bug2.X
}

type Intf interface {
	Get() []byte
}

func main() {
	x := &bug2.X{T: [32]byte{1,2,3,4}}
	var ix Intf = X{x}
	t1 := ix.Get()
	t2 := x.Get()
	if !bytes.Equal(t1, t2) {
		fmt.Printf("failure: got %x want %x", t1, t2)
	}
}
