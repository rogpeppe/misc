// This program runs a one-dimensional cellular automaton.
//
// Written to accompany this Go meetup:
//
//	http://www.meetup.com/Golang-North-East/events/231080137/
package main

import (
	"flag"
	"fmt"
	"github.com/rogpeppe/misc/linedrawer"
	"log"
	"math/rand"
	"os"
	"time"
)

var (
	sleepTime  = flag.Duration("t", 500*time.Microsecond, "time to sleep between generating each row")
	cellRadius = flag.Int("r", 1, "radius - number of cells either side to consider")
	numStates  = flag.Int("s", 0, "number of states (defaults to one more than largest value provided")
	numCells   = flag.Int("n", 1000, "number of cells")
)

var usage = `usage: lineca [flags] rules

The rule is specified as a base-n (n is number of states) number, 
where the n'th digit (with the least significant digit as 0)
gives the next state of a cell whose neighbourhood cells
sum to n.

For example:

	lineca 3311100320
	lineca 3311200320
`

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, usage)
		flag.PrintDefaults()
		os.Exit(2)
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
	}
	next, err := parseRule(flag.Arg(0), *cellRadius, *numStates)
	if err != nil {
		log.Fatal(err)
	}
	linedrawer.Main(func(newLineDrawer linedrawer.NewFunc) {
		d, err := newLineDrawer(*numCells, 4)
		if err != nil {
			log.Fatal(err)
		}
		ca := &ca{
			r:    *cellRadius,
			n:    *numCells,
			next: next,
			c0:   make([]int, *numCells),
			c1:   make([]int, *numCells),
		}
		for i := range ca.c0 {
			ca.c0[i] = rand.Intn(4)
		}
		for i := 0; ; i++ {
			d.DrawLine(ca.c0)
			ca.step()
			if *sleepTime != 0 {
				time.Sleep(*sleepTime)
			}
		}
	})
	select {}
}

func parseRule(s string, radius int, numStates int) ([]int, error) {
	if len(s) == 0 {
		return nil, fmt.Errorf("empty ruleset")
	}
	r := make([]int, len(s))
	max := 0

	for i, c := range s {
		if c < '0' || c > '9' {
			return nil, fmt.Errorf("non-digit state value '%c' in ruleset", c)
		}
		d := int(c) - '0'
		if d > max {
			max = d
		}
		r[i] = d
	}
	if numStates == 0 {
		numStates = max + 1
	} else if max >= numStates {
		return nil, fmt.Errorf("cell state out of range")
	}
	maxVal := (numStates - 1) * (radius*2 + 1)
	rule := make([]int, maxVal+1)
	if len(r) > len(rule) {
		return nil, fmt.Errorf("too many states specified (need %d)", maxVal+1)
	}
	j := 0
	for i := len(r) - 1; i >= 0; i-- {
		rule[j] = r[i]
		j++
	}
	return rule, nil
}

type ca struct {
	r      int
	n      int
	next   []int
	c0, c1 []int
}

func (c *ca) step() {
	for i := range c.c1 {
		sum := 0
		for j := i - c.r; j <= i+c.r; j++ {
			sum += c.c0[(j+c.n)%c.n]
		}
		c.c1[i] = c.next[sum]
	}
	c.c0, c.c1 = c.c1, c.c0
}
