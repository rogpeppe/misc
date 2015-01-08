// fc - floating point reverse polish notation calculator
// version 2 - rewritten -- wrtp  1/91
// ansified, swap, rep and dup added wrtp 9/95
// bugs, comments, etc to roger peppe (rog@ohm.york.ac.uk)
// version 4 - Goifed, yeah!
package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type op struct {
	name string
	f    interface{}
}

var ops = []op{
	{"pi", math.Pi},
	{"e", math.E},
	{"nan", math.NaN()},
	{"NaN", math.NaN()},
	{"infinity", math.Inf(1)},
	{"Infinity", math.Inf(1)},
	{"inf", math.Inf(1)},
	{"∞", math.Inf(1)},
	{"swap", swap},
	{"dup", dup},
	{"rep", rep},
	{"!", factorial},
	{"%", mod},
	{"p", printNum},
	{"*", mult},
	{"**", math.Pow},
	{"+", plus},
	{"-", minus},
	{"/", div},
	{"^", math.Pow},
	{"_", uminus},
	{">>", shiftr},
	{"shr", shiftr},
	{"<<", shiftl},
	{"shl", shiftl},
	{"and", and},
	{"or", or},
	{"xor", xor},
	{"not", not},
	{"sum", sum},
	{"acos", math.Acos},
	{"asin", math.Asin},
	{"atan", math.Atan},
	{"atan2", math.Atan2},
	{"ceil", math.Ceil},
	{"cos", math.Cos},
	{"cosh", math.Cosh},
	{"deg", degree},
	{"exp", math.Exp},
	{"fabs", math.Abs},
	{"floor", math.Floor},
	{"fmod", math.Mod},
	{"ldexp", math.Ldexp},
	{"log", math.Log},
	{"ln", math.Log},
	{"log10", math.Log10},
	{"log2", log2},
	{"pow", math.Pow},
	{"rad", radian},
	{"sin", math.Sin},
	{"sinh", math.Sinh},
	{"sqrt", math.Sqrt},
	{"tan", math.Tan},
	{"tanh", math.Tanh},
	{"x", mult},
	{"xx", math.Pow},
}

type cvt struct {
	pat *regexp.Regexp
	cvt func(s string) float64
}

var conversions = []cvt{
	{
		regexp.MustCompile("^0[bB][01]+$"),
		func(s string) float64 {
			return float64(btoi(s[2:], 2))
		},
	},
	{
		regexp.MustCompile("^0[xX][0-9a-fA-F]+$"),
		func(s string) float64 {
			return float64(btoi(s[2:], 16))
		},
	},
	{
		regexp.MustCompile("^0[0-7]+$"),
		func(s string) float64 {
			return float64(btoi(s[1:], 8))
		},
	},
	{
		regexp.MustCompile(`^(([0-9]+(\.[0-9]+)?)|([0-9]*(\.[0-9]+)))([eE][\-+]?[0-9]+)?$`),
		func(s string) (v float64) {
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				fatalf("bad number %q", s)
			}
			return
		},
	},
	{
		regexp.MustCompile("^@.$"),
		func(s string) float64 {
			for _, c := range s[1:] {
				return float64(c)
			}
			panic("not reached")
		},
	},
}

var stack []float64
var lastOp *op // used by rep operation

const (
	_ = iota
	dec
	bin
	annotbin
	oct
	hex
	char
)

var base = dec

func usage() {
	b := new(bytes.Buffer)
	fmt.Fprintf(b, "Usage: fc -[bBoxcd] <postfix expression>\n")
	fmt.Fprintf(b, "Operands are decimal(default), hex(0x), octal(0), binary(0b),\n")
	fmt.Fprintf(b, "             char(@), time(hh:mm.ss)\n")
	fmt.Fprintf(b, "Operators are:\n")
	cols := 0
	for _, o := range ops {
		b.WriteString(o.name)
		b.WriteRune(' ')
		cols += len(o.name) + 1
		if cols > 60 {
			fmt.Fprintf(b, "\n")
			cols = 0
		}
	}
	if cols > 0 {
		fmt.Fprintf(b, "\n")
	}
	os.Stderr.Write(b.Bytes())
	os.Exit(2)
}

func main() {
	args := os.Args
	if len(args) < 2 {
		return
	}
	args = args[1:]
	a := args[0]
	if len(a) > 1 && a[0] == '-' && !isNumber(a) {
		switch a[1] {
		case 'd':
			base = dec
		case 'x':
			base = hex
		case 'o':
			base = oct
		case 'b':
			base = bin
		case 'c':
			base = char
		case 'B':
			base = annotbin
		default:
			fmt.Fprintf(os.Stderr, "fc: unknown option -%c\n", a[1])
			usage()
		}
		args = args[1:]
	}

	// push numbers; execute operations
	for ; len(args) > 0; args = args[1:] {
		s := args[0]
		ok, v := number(s)
		if ok {
			push(v)
		} else {
			op := find(s)
			if op == nil {
				fatalf("unknown operator %q", s)
			}
			op.exec()
			lastOp = op
		}
	}

	// print stack bottom first
	for _, v := range stack {
		printNum(v)
	}
}

func printNum(v float64) float64 {
	fmt.Println(numToStr(v))
	return v
}

func numToStr(v float64) string {
	switch base {
	case char:
		return fmt.Sprintf("@%c", int(v))
	case dec:
		return fmt.Sprintf("%g", v)
	case bin:
		return numToBinary(int64(v))
	case annotbin:
		return numToAnnotatedBinary(int64(v))
	case oct:
		return fmt.Sprintf("%#o", int64(v))
	case hex:
		return fmt.Sprintf("%#x", int64(v))
	}
	fatalf("unknown base %d", base)
	panic("not reached")
}

// numToBinary returns  n as a binary number, always producing
// a multiple of 8 binary digits.
func numToBinary(v int64) string {
	s := strconv.FormatInt(v, 2)
	w := (len(s) + 7) / 8
	if w == 0 {
		w = 8
	}
	return pad(s, w, '0')
}

// pad pads the string s to width w with rune c.
func pad(s string, w int, c int) string {
	if w <= len(s) {
		return s
	}
	return s + strings.Repeat(string(c), w-len(s))
}

func numToAnnotatedBinary(n int64) string {
	b := new(bytes.Buffer)
	s := numToBinary(n)
	ndig := len(s)
	b.WriteString(s)
	b.WriteString("\n")
	for i := ndig - 1; i >= 0; i-- {
		b.WriteRune('0' + rune(i)%10)
	}
	if ndig < 10 {
		return b.String()
	}
	b.WriteRune('\n')
	for i := ndig - 1; i >= 10; i-- {
		if i%10 == 0 {
			b.WriteRune('0' + rune(i)/10)
		} else {
			b.WriteString(" ")
		}
	}
	return b.String()
}

func ensure(n int, o *op) {
	if len(stack) < n {
		fatalf("Stack too small for op %q", o.name)
	}
}

func (o *op) exec() {
	switch f := o.f.(type) {
	case float64:
		push(f)
	case func(float64) float64:
		ensure(1, o)
		push(f(pop()))
	case func(int64) int64:
		ensure(1, o)
		push(float64(f(round(pop()))))
	case func(float64, float64) float64:
		ensure(2, o)
		y, x := pop(), pop()
		push(f(x, y))
	case func(int64, int64) int64:
		ensure(2, o)
		y, x := pop(), pop()
		push(float64(f(round(x), round(y))))
	case func([]float64) []float64:
		stack = f(stack)
	default:
		fatalf("unknown operation type: %T", f)
	}
}

func round(x float64) int64 {
	if x < 0 {
		x -= 0.5
	} else {
		x += 0.5
	}
	return int64(x)
}

func push(v float64) {
	stack = append(stack, v)
}

func pop() float64 {
	v := stack[len(stack)-1]
	stack = stack[0 : len(stack)-1]
	return v
}

func find(s string) *op {
	neg := len(s) > 1 && s[0] == '-'
	if neg {
		s = s[1:]
	}
	for i, o := range ops {
		if s == o.name {
			if !neg {
				return &ops[i]
			}
			// deal with negative versions of constants.
			if v, ok := o.f.(float64); ok {
				o.f = -v
				return &o
			}
		}
	}
	return nil
}

func isNumber(s string) bool {
	if len(s) > 0 && s[0] == '-' {
		s = s[1:]
	}
	for _, x := range conversions {
		if x.pat.MatchString(s) {
			return true
		}
	}
	o := find(s)
	if o == nil {
		return false
	}
	_, ok := o.f.(float64)
	return ok
}

func number(s string) (bool, float64) {
	neg := len(s) > 0 && s[0] == '-'
	if neg {
		s = s[1:]
	}
	for _, x := range conversions {
		if x.pat.MatchString(s) {
			x := x.cvt(s)
			if neg {
				x = -x
			}
			return true, x
		}
	}
	return false, 0
}

func btoi(s string, base int) int64 {
	i, err := strconv.ParseInt(s, base, 64)
	if err != nil {
		fatalf("bad number %q", s)
	}
	return i
}

func swap(p []float64) []float64 {
	n := len(p) - 1
	if len(p) >= 2 {
		p[n], p[n-1] = p[n-1], p[n]
	}
	return p
}

func dup(p []float64) []float64 {
	if len(p) < 0 {
		fatalf("Stack too small for op %q", dup)
	}
	return append(p, p[len(p)-1])
}

// repeat last operator until not enough elements left on stack
func rep(p []float64) []float64 {
	if lastOp == nil {
		fatalf("No operator to rep")
	}
	switch lastOp.f.(type) {
	case func(float64, float64) float64:
	case func(int64, int64) int64:
	default:
		fatalf("Invalid operator for rep")
	}
	for len(stack) > 1 {
		lastOp.exec()
	}
	return stack
}

// sum all elements on stack - same as + rep */
func sum(p []float64) []float64 {
	v := 0.0
	for _, x := range p {
		v += x
	}
	return []float64{v}
}

func mod(x, y int64) int64 {
	return x % y
}
func plus(x, y float64) float64 {
	return x + y
}
func minus(x, y float64) float64 {
	return x - y
}
func mult(x, y float64) float64 {
	return x * y
}
func div(x, y float64) float64 {
	return x / y
}
func uminus(x int64) int64 {
	return -x
}
func factorial(x int64) int64 {
	v := int64(1)
	for ; x > 0; x-- {
		v *= x
	}
	return v
}
func degree(x float64) float64 {
	return x / (2 * math.Pi) * 360
}
func radian(x float64) float64 {
	return (x / 360) * 2 * math.Pi
}
func and(x, y int64) int64 {
	return x & y
}
func or(x, y int64) int64 {
	return x | y
}
func xor(x, y int64) int64 {
	return x ^ y
}
func not(x int64) int64 {
	return ^x
}
func shiftl(x, y int64) int64 {
	return x << uint(y)
}
func shiftr(x, y int64) int64 {
	return x >> uint(y)
}
func log2(x float64) float64 {
	return math.Log2(x)
}

func fatalf(f string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "fc: %s\n", fmt.Sprintf(f, a...))
	os.Exit(2)
}
