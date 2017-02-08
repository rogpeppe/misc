// The httpproxy command implements a very simple password-authenticated letsencrypt-based
// HTTPS terminator for a statically known set of services (typically internal).
//
// Currently in hack-it-up-in-one-hour state.
package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"

	"golang.org/x/crypto/acme/autocert"
)

type config struct {
	Hosts    map[string]string `json:"hosts"`
	Password string            `json:"password"`
	Port     int
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
		"host1.ddns.net": "192.168.2.99:8080",
		"host2.ddns.net": "192.168.2.101:80",
		"host3.ddns.net": "192.168.2.100:80"
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
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(*cacheDir),
		HostPolicy: autocert.HostWhitelist(cfg.allHosts()...),
		//		Client: &acme.Client{
		//			DirectoryURL: "https://acme-staging.api.letsencrypt.org/directory",
		//		},
	}
	tlsConfig := &tls.Config{
		GetCertificate: func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			log.Printf("getting certificate for server name %q", clientHello.ServerName)
			// Get the locally created certificate and whether it's appropriate
			// for the SNI name. If not, we'll try to get an acme cert and
			// fall back to the local certificate if that fails.
			if !strings.HasSuffix(clientHello.ServerName, ".acme.invalid") {
				if _, ok := cfg.Hosts[clientHello.ServerName]; !ok {
					return nil, fmt.Errorf("unknown site %q (all hosts %q)", clientHello.ServerName, cfg.allHosts())
				}
			}
			acmeCert, err := m.GetCertificate(clientHello)
			if err == nil {
				return acmeCert, nil
			}
			log.Printf("cannot get autocert certificate for %q: %v", clientHello.ServerName, err)
			return nil, err
		},
	}
	srv := &server{
		errorURL: httptest.NewServer(http.HandlerFunc(errorHandler)).URL,
		cfg:      &cfg,
	}
	if cfg.Port == 0 {
		cfg.Port = 443
	}
	httpSrv := &http.Server{
		Addr:      fmt.Sprintf(":%d", cfg.Port),
		Handler:   srv.handler(),
		TLSConfig: tlsConfig,
	}
	err := httpSrv.ListenAndServeTLS("", "")
	log.Fatalf("server exited: %v", err)
}

func errorHandler(w http.ResponseWriter, req *http.Request) {
	switch req.URL.Path {
	case "/badcreds":
		w.Write([]byte("bad username/password"))
	case "/badhost":
		w.Write([]byte("bad host!"))
	default:
		w.Write([]byte("why are you here?"))
	}
}

type server struct {
	tlsConfig *tls.Config
	cfg       *config
	errorURL  string
}

const cookieName = "httpproxy-dsafvljfnqpeoifnldavldksjnsa" // TODO think

func (srv *server) handler() http.Handler {
	director := func(h http.Header, req *http.Request) {
		log.Printf("request %v", req.URL)
		defer func() {
			log.Printf("directed to %v", req.URL)
		}()
		cookie, err := req.Cookie(cookieName)
		authed := false
		if err == nil {
			authed = cookie.Value == srv.cfg.Password
			if !authed {
				log.Printf("cookie auth failed; value %q", cookie.Value)
			}
		}
		req.URL.Scheme = "http"
		if !authed {
			req.ParseForm()
			// TODO form value might clash!
			// TODO distinguish between expired creds and wrong creds?
			if req.Form.Get("pass") == srv.cfg.Password {
				setCookie(h, &http.Cookie{
					Name:   cookieName,
					Value:  srv.cfg.Password,  // TODO should probably not store password in plaintext.
					MaxAge: 28 * 24 * 60 * 60, // a month
				})
				authed = true
				delete(req.Form, "pass")
			}
		}
		if !authed {
			req.URL = mustParseURL(srv.errorURL + "/badcreds")
		}
		newHost, ok := srv.cfg.Hosts[req.Host]
		if !ok {
			log.Printf("unknown host %q", req.Host)
			req.URL = mustParseURL(srv.errorURL + "/badhost")
			return
		}
		req.URL.Host = newHost
	}
	return &ReverseProxy{
		Director: director,
	}
}

func setCookie(h http.Header, cookie *http.Cookie) {
	if v := cookie.String(); v != "" {
		h.Add("Set-Cookie", v)
	}
}
func mustParseURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		log.Fatalf("cannot parse url %q: %v", err)
	}
	return u
}

func (cfg *config) allHosts() []string {
	var hs []string
	for h := range cfg.Hosts {
		hs = append(hs, h)
	}
	return hs
}
