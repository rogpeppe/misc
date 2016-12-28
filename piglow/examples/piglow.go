package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/rogpeppe/misc/piglow"
	"golang.org/x/exp/io/i2c"
)

var reset = flag.Bool("r", false, "start all LEDs from afresh")

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: piglow [flags] level group|operator...\n`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
The piglow command sets a set of PiGlow LEDs to the given
level (0-255).

Each argument after the level operates on a stack, each element
of which holds a set of LEDs. Elements are added by proceeding
through all the arguments in sequence. After all arguments are
processed, the union of all LEDs in the stack will be set.

The following arguments are recognized:

	all - all the LEDs.
	<color name> - all the LEDs with the given color.
	<number> - the LED with the given number (0..17)
	r<number> - all LEDs the given radius from the centre (0..6)
	arm<number> - all LEDs in the given arm (0..2)
	<number>..<number> - all LEDs in the given numeric range
	r<number>..r<number> - all LEDs in the given radius range
	+ - the union of the top two stack elements
	- - the difference of the top two stack elements
	. - the intersection of the top two stack elements

For example:

	piglow 50 arm1 red white + .

will set the red and white LEDs on arm 1 to level 50.

	piglow 10 all white -

will set all except the white LEDs to level 10.
`[1:])
		os.Exit(1)
	}
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
	}
	p, err := piglow.Open(&i2c.Devfs{Dev: "/dev/i2c-1"})
	if err != nil {
		log.Fatalf("cannot open: %v", err)
	}
	if *reset {
		if err := p.Setup(); err != nil {
			log.Fatalf("cannot reset: %v", err)
		}
	}
	levelStr := flag.Arg(0)
	level, err := strconv.Atoi(levelStr)
	if err != nil || level < 0 || level >= 256 {
		log.Fatalf("invalid level %q", levelStr)
	}
	var stack []piglow.Set
	for _, arg := range flag.Args()[1:] {
		if op := ops[arg]; op != nil {
			stack1, err := op(stack)
			if err != nil {
				log.Fatalf("operation %q: %v", arg, err)
			}
			stack = stack1
		} else {
			g, err := piglow.ParseGroup(arg)
			if err != nil {
				log.Fatal(err)
			}
			stack = append(stack, g.LEDs())
		}
	}
	var set piglow.Set
	for _, s := range stack {
		set |= s
	}
	if err := p.SetBrightness(set, uint8(level)); err != nil {
		log.Fatal(err)
	}
}

func binOp(f func(piglow.Set, piglow.Set) piglow.Set) func([]piglow.Set) ([]piglow.Set, error) {
	return func(stack []piglow.Set) ([]piglow.Set, error) {
		if len(stack) < 2 {
			return nil, fmt.Errorf("stack too small")
		}
		v0, v1 := stack[len(stack)-2], stack[len(stack)-1]
		stack = append(stack[0:len(stack)-2], f(v0, v1))
		return stack, nil
	}
}

var ops = map[string]func([]piglow.Set) ([]piglow.Set, error){
	"+": binOp(func(s0, s1 piglow.Set) piglow.Set {
		return s0 | s1
	}),
	"-": binOp(func(s0, s1 piglow.Set) piglow.Set {
		return s0 &^ s1
	}),
	".": binOp(func(s0, s1 piglow.Set) piglow.Set {
		return s0 & s1
	}),
}
