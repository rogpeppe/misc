package httpguard

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strings"

	"gopkg.in/errgo.v1"
)

func serveWSProxy(target target, w http.ResponseWriter, r *http.Request) error {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return errgo.New("not a hijacker?")
	}
	var d net.Conn
	var err error
	switch target.scheme {
	case "https":
		d, err = tls.Dial("tcp", target.host, nil)
	case "http":
		d, err = net.Dial("tcp", target.host)
	default:
		panic("unreachable")
	}
	if err != nil {
		return errgo.Notef(err, "error dialing websocket backend %s", target)
	}

	nc, _, err := hj.Hijack()
	if err != nil {
		return errgo.Notef(err, "hijack error")
	}
	defer nc.Close()
	defer d.Close()

	err = r.Write(d)
	if err != nil {
		return errgo.Notef(err, "error copying request to target")
	}

	errc := make(chan error, 2)
	cp := func(dst io.Writer, src io.Reader) {
		_, err := io.Copy(dst, src)
		errc <- err
	}
	go cp(d, nc)
	go cp(nc, d)
	<-errc
	return nil
}

func isWebsocket(h http.Header) bool {
	return strings.EqualFold(h.Get("Connection"), "upgrade") && strings.EqualFold(h.Get("Upgrade"), "websocket")
}
