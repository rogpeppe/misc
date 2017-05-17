// The httpproxy command implements a very simple password-authenticated letsencrypt-based
// HTTPS terminator for a statically known set of services (typically internal).
//
// Currently in hack-it-up-in-one-hour state.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/rogpeppe/misc/httpguard"
	"golang.org/x/crypto/acme/autocert"
)

type config struct {
	Hosts    map[string]string `json:"hosts"`
	Password string            `json:"password"`
	Port     int               `json:"port"`
}

var cacheDir = flag.String("d", "/tmp/autocert", "certificate directory cache")

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `
usage: httpproxy <config>

Example config:
{
	"password": "foo",
	"port": 8080,
	"hosts": {
		"host1.ddns.net": "http://192.168.2.99:8080",
		"host2.ddns.net": "http://192.168.2.101:80",
		"host3.ddns.net": "http://192.168.2.100:80"
	}
}
`[1:])

		os.Exit(2)
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
	}
	var cfg config
	if err := json.Unmarshal([]byte(flag.Arg(0)), &cfg); err != nil {
		log.Fatal("bad config: ", err)
	}
	m := autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache(*cacheDir),
		//		Client: &acme.Client{
		//			DirectoryURL: "https://acme-staging.api.letsencrypt.org/directory",
		//		},
	}
	p := httpguard.Params{
		Hosts:           cfg.Hosts,
		Port:            cfg.Port,
		Password:        cfg.Password,
		AutocertManager: &m,
	}
	log.Fatal("server exited: ", httpguard.Serve(p))
}
