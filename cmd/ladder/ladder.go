package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"text/template"
)

type tmplArgs struct {
	Labels []Label
	Arrows []Arrow
}

type Label struct {
	Name string
	Text []string
}

type Arrow struct {
	From string
	To   string
	Text []string
}

var ladderPic = `.PS
{{range $label := .Labels -}}
{{$label.Name}}: box{{range $label.Text}} {{printf "%q" .}}{{end}} width 1.5
move right 1
{{end}}
{{range $label := .Labels -}}
line down {{vert (len $.Arrows)}} dashed from {{$label.Name}}.s
{{end}}
{{range $n, $arrow := .Arrows -}}
{{$v := vert $n}}
move to {{$arrow.From}}.s down {{$v}}
arrow to {{$arrow.To}}.s down {{$v}} {{range $arrow.Text}} {{printf "%q" .}} {{end}}
{{- if lt (len $arrow.Text) 2}}above{{end}}
{{end}}
.PE
`

var usageMessage = `
Usage: ladder [file]

The ladder command reads a ladder diagram specification
from the given file (or stdin if no file is given) and
writes the result in pic format to stdout.

The format consists of two sections. The first section
holds a line for each column with the label for the column
(which must start with a capital letter), then a space
and the column title.

For example:

	Foo some text

The second section starts with a blank line, then
any number of lines of the form:

	<from> <to> <text>

which displays an arrow between the from and to labels
with the given text displayed along the arrow.

In all text, a literal \n (backslash-n) can be used to
break the text into multiple lines.

A full example:

	Client Client v2
	Discharger Discharger v2
	Server Server v1
	
	Client Server Do request (v2)
	Server Client Discharge-required error\nwith v1 macaroon with old-style caveat id
	Client Discharger POST /discharge (v2)
	Discharger Client Discharge v2 macaroon
	Client Server Do request (v2, v1 macaroon + v1 discharge)
`[1:]

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s", usageMessage)
		os.Exit(2)
	}
	flag.Parse()
	var tmplData []byte
	switch flag.NArg() {
	case 0:
		data, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			log.Fatal(err)
		}
		tmplData = data
	case 1:
		data, err := ioutil.ReadFile(flag.Arg(0))
		if err != nil {
			log.Fatal(err)
		}
		tmplData = data
	default:
		flag.Usage()
	}
	args, err := parse(string(tmplData))
	if err != nil {
		log.Fatal("cannot parse ladder description: ", err)
	}
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"vert": vert,
	}).Parse(ladderPic)
	if err != nil {
		log.Fatal("cannot parse template: ", err)
	}
	if err := tmpl.Execute(os.Stdout, &args); err != nil {
		log.Fatal("cannot execute template: ", err)
	}
}

func vert(x int) string {
	return fmt.Sprintf("%.1f", float64(x)*0.5+0.5)
}

/* example:
Client Client v3
Server Server v3
Discharger Discharger v3

Client Server message 1
Server Client message 2
Server Client message 3


Client Client v1
Server Server v1
Discharger Discharger v1

Client Server Do request
Server Client Discharge required error\nwith macaroon
Client Discharger Discharge
Discharger Client Discharge macaroon
Client Server Do request (macaroon + discharge)
*/

func parse(s string) (*tmplArgs, error) {
	var args tmplArgs
	lines := strings.Split(s, "\n")
	var rest []string
	names := make(map[string]bool)
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			rest = lines[i+1:]
			break
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("bad label line %q", line)
		}
		names[parts[0]] = true
		args.Labels = append(args.Labels, Label{
			Name: parts[0],
			Text: strings.Split(parts[1], "\\n"),
		})
	}
	for _, line := range rest {
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 3 {
			return nil, fmt.Errorf("bad arrow line %q", line)
		}
		if !names[parts[0]] || !names[parts[1]] {
			return nil, fmt.Errorf("bad name %q or %q", parts[0], parts[1])
		}
		args.Arrows = append(args.Arrows, Arrow{
			From: parts[0],
			To:   parts[1],
			Text: strings.Split(parts[2], "\\n"),
		})
	}
	return &args, nil
}
