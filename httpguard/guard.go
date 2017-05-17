// Package implements a very simple password-authenticated letsencrypt-based
// HTTPS terminator for a statically known set of services (typically internal).
package httpguard

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"golang.org/x/crypto/acme/autocert"
	"gopkg.in/errgo.v1"
)

type Params struct {
	// Hosts holds a map from virtual hostname
	// to the destination target URL. The URL scheme
	// may only be "http" or "https" and its path
	// must be empty.
	Hosts           map[string]string
	// Password holds the password that the
	// client must provide to gain access.
	// If this is empty, no authentication will take
	// place - httpguard will just act as a proxy.
	Password        string `json:"password"`
	// Port holds the port to listen on.
	Port            int
	// AutocertManager holds the autocert manager to use.
	// It should at least have Prompt and Cache set.
	AutocertManager *autocert.Manager
}

type params struct {
	targets map[string]target
	Params
}

type target struct {
	scheme string
	host   string
}

// Serve starts serving the httpguard server.
// It only returns when the server has stopped.
func Serve(p0 Params) error {
	p := params{
		Params: p0,
	}
	targets, err := parseURLs(p.Hosts)
	if err != nil {
		return errgo.Mask(err)
	}
	p.targets = targets
	if p.AutocertManager == nil {
		return errgo.New("no autocert manager provided")
	}
	m := *p.AutocertManager
	p.AutocertManager = &m
	p.AutocertManager.HostPolicy = autocert.HostWhitelist(p.allHosts()...)
	if p.Port == 0 {
		p.Port = 443
	}

	tlsConfig := &tls.Config{
		GetCertificate: func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			log.Printf("getting certificate for server name %q", clientHello.ServerName)
			// Get the locally created certificate and whether it's appropriate
			// for the SNI name. If not, we'll try to get an acme cert and
			// fall back to the local certificate if that fails.
			if !strings.HasSuffix(clientHello.ServerName, ".acme.invalid") {
				if _, ok := p.Hosts[clientHello.ServerName]; !ok {
					return nil, fmt.Errorf("unknown site %q (all hosts %q)", clientHello.ServerName, p.allHosts())
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
	srv := newServer(p)
	httpSrv := &http.Server{
		Addr:      fmt.Sprintf(":%d", p.Port),
		Handler:   srv,
		TLSConfig: tlsConfig,
	}
	return httpSrv.ListenAndServeTLS("", "")
}

type server struct {
	tlsConfig *tls.Config
	p         params
	proxy     http.Handler
}

func newServer(p params) *server {
	srv := &server{
		p: p,
	}
	srv.proxy = &httputil.ReverseProxy{
		Director: srv.director,
	}
	return srv
}

func (srv *server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if err := srv.auth(w, req); err != nil {
		log.Printf("auth error: %v", err)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if isWebsocket(req.Header) {
		if err := serveWSProxy(srv.p.targets[req.Host], w, req); err != nil {
			log.Printf("wsproxy error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		return
	}
	srv.proxy.ServeHTTP(w, req)
}

const cookieName = "httpproxy-dsafvljfnqpeoifnldavldksjnsa" // TODO think

func (srv *server) auth(w http.ResponseWriter, req *http.Request) error {
	if srv.p.Password == "" {
		return nil
	}
	cookie, err := req.Cookie(cookieName)
	if err == nil {
		log.Printf("cookie password %q", cookie.Value)
		if cookie.Value == srv.p.Password {
			return nil
		}
		log.Printf("cookie auth failed; value %q", cookie.Value)
	}
	if err := srv.passwordAuth(w, req); err != nil {
		return errgo.Mask(err)
	}
	if _, ok := srv.p.targets[req.Host]; !ok {
		return errgo.Newf("unknown host %q", req.Host)
	}
	return nil
}

func (srv *server) passwordAuth(w http.ResponseWriter, req *http.Request) error {
	values, _ := url.ParseQuery(req.URL.RawQuery)
	log.Printf("url password %q", values.Get("pass"))
	// TODO serve up a password-entry form instead.
	// TODO distinguish between expired creds and wrong creds?
	if values.Get("pass") != srv.p.Password {
		return errgo.New("invalid password")
	}
	setCookie(w.Header(), &http.Cookie{
		Name:   cookieName,
		Value:  srv.p.Password,    // TODO should probably not store password in plaintext.
		MaxAge: 28 * 24 * 60 * 60, // a month
	})
	return nil
}

func (srv *server) director(req *http.Request) {
	log.Printf("request %v", req.URL)
	defer func() {
		log.Printf("directed to %v", req.URL)
	}()
	target, ok := srv.p.targets[req.Host]
	if !ok {
		panic("unexpected host - this should have been checked earlier")
	}
	req.URL.Scheme = target.scheme
	req.URL.Host = target.host
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

func (p *params) allHosts() []string {
	var hs []string
	for h := range p.Hosts {
		hs = append(hs, h)
	}
	return hs
}

func parseURLs(urls map[string]string) (map[string]target, error) {
	targets := make(map[string]target)
	for name, urlStr := range urls {
		u, err := url.Parse(urlStr)
		if err != nil {
			return nil, errgo.Mask(err)
		}
		t := target{
			host:   u.Host,
			scheme: u.Scheme,
		}
		if u.Path != "/" && u.Path != "" {
			return nil, errgo.Newf("url %q must not contain a path", urlStr)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, errgo.Newf("url %q has bad scheme", u)
		}
		if t.host == "" {
			return nil, errgo.Newf("url %q has no host", u)
		}
		if _, _, err := net.SplitHostPort(t.host); err != nil {
			switch t.scheme {
			case "http":
				t.host += ":80"
			case "https":
				t.host += ":443"
			}
		}
		targets[name] = t
	}
	return targets, nil
}
