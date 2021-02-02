package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	flag "github.com/juju/gnuflag"
)

var (
	url           = flag.Bool("url", false, "use URL encoding")
	raw           = flag.Bool("raw", false, "no padding")
	decode        = flag.Bool("d", false, "decode data")
	wrap          = flag.Int("w", 76, "wrap encoded lines after this many columns (0 to disable)")
	ignoreGarbage = flag.Bool("ignore-garbage", false, "when decoding, ignore non-alphabet characters")
)

func init() {
	flag.BoolVar(decode, "decode", false, "")
	flag.IntVar(wrap, "wrap", 76, "")
}

type enc struct {
	url bool
	raw bool
}

var encodings = map[enc]*base64.Encoding{
	{false, false}: base64.StdEncoding,
	{false, true}:  base64.RawStdEncoding,
	{true, false}:  base64.URLEncoding,
	{true, true}:   base64.RawURLEncoding,
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `
Usage: /usr/bin/base64 [OPTION]... [FILE]
Base64 encodes or decodes FILE, or standard input, to standard output.

With no FILE, or when FILE is -, read standard input.

The data are encoded as described for both the base64 alphabets in RFC 4648.

When decoding, the input may contain newlines in addition to the bytes
of the formal base64 alphabet, and either encoding described in RFC 4648
("standard" or "URL-safe") is accepted, with or without padding characters
(any "=" character in the input is ignored).

Use --ignore-garbage to attempt to recover from any other non-alphabet
bytes in the encoded stream.
`[1:])
		flag.PrintDefaults()
		os.Exit(1)
	}

	flag.Parse(false)
	if *decode {
		if err := decodeBase64(); err != nil {
			fmt.Fprintf(os.Stderr, "decode error: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := encodeBase64(); err != nil {
			fmt.Fprintf(os.Stderr, "encode error: %v\n", err)
			os.Exit(1)
		}
	}
}

func decodeBase64() error {
	if *ignoreGarbage {
		for i := range garbage {
			garbage[i] = strings.IndexByte(nonGarbage, byte(i)) == -1
		}
		fmt.Fprintf(os.Stderr, "garbage: %v\n", garbage)
	}
	var r io.Reader
	switch {
	case flag.NArg() > 1:
		return fmt.Errorf("too many arguments")
	case flag.NArg() == 0 || flag.Arg(0) == "-":
		r = os.Stdin
	default:
		r1, err := os.Open(flag.Arg(0))
		if err != nil {
			return err
		}
		defer r1.Close()
		r = r1
	}
	dr := base64.NewDecoder(base64.RawStdEncoding, translateReader{r})
	if _, err := io.Copy(os.Stdout, dr); err != nil {
		return err
	}
	return nil
}

func encodeBase64() error {
	encoding := encodings[enc{*url, *raw}]
	w := bufio.NewWriter(os.Stdout)
	out := io.Writer(w)
	if *wrap > 0 {
		out = &wrappingWriter{w: w}
	}
	b64w := base64.NewEncoder(encoding, out)
	n, err := io.Copy(b64w, os.Stdin)
	if err != nil {
		return err
	}
	if err := b64w.Close(); err != nil {
		return err
	}
	if n > 0 {
		if _, err := out.Write(nl); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return nil
}

type translateReader struct {
	r io.Reader
}

func (r translateReader) Read(buf []byte) (int, error) {
	n, err := r.r.Read(buf)
	if n == 0 {
		return n, err
	}
	buf = buf[0:n]
	hasGarbage := false
	for i, b := range buf {
		switch b {
		case '-':
			buf[i] = '+'
		case '_':
			buf[i] = '/'
		}
		hasGarbage = hasGarbage || garbage[b]
	}
	if !hasGarbage {
		return n, err
	}
	j := 0
	for _, b := range buf {
		if !garbage[b] {
			buf[j] = b
			j++
		}
	}
	return j, err
}

var garbage = [256]bool{
	' ':  true,
	'\n': true,
	'\r': true,
	'\t': true,
	'=':  true,
}

var nonGarbage = `ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/-_`

type wrappingWriter struct {
	w   io.Writer
	col int
}

var nl = []byte("\n")

func (w *wrappingWriter) Write(buf []byte) (int, error) {
	total := len(buf)
	for w.col+len(buf) > *wrap {
		n := *wrap - w.col
		if _, err := w.w.Write(buf[:n]); err != nil {
			return 0, err
		}
		if _, err := w.w.Write(nl); err != nil {
			return 0, err
		}
		w.col = 0
		buf = buf[n:]
	}
	if len(buf) > 0 {
		if _, err := w.w.Write(buf); err != nil {
			return 0, err
		}
		w.col += len(buf)
	}
	return total, nil
}
