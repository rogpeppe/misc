package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: number string\nreplace all occurrences of string by sequential numbers\n")
		os.Exit(2)
	}
	scan := bufio.NewScanner(os.Stdin)
	n := int64(0)
	token := []byte(os.Args[1])
	var buf []byte
	for scan.Scan() {
		line := scan.Bytes()
		parts := bytes.Split(line, token)
		buf = append(buf[:0], parts[0]...)
		for _, part := range parts[1:] {
			buf = strconv.AppendInt(buf, n, 10)
			n++
			buf = append(buf, part...)
		}
		buf = append(buf, '\n')
		os.Stdout.Write(buf)
	}
}
