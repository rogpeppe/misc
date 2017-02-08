package main

import (
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func serveWSProxy(target *url.URL, w http.ResponseWriter, r *http.Request) {
	d, err := net.Dial("tcp", target.Host)
	if err != nil {
		http.Error(w, "Error contacting backend server.", 500)
		log.Printf("Error dialing websocket backend %s: %v", target, err)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Not a hijacker?", 500)
		return
	}
	nc, _, err := hj.Hijack()
	if err != nil {
		log.Printf("Hijack error: %v", err)
		return
	}
	defer nc.Close()
	defer d.Close()

	err = r.Write(d)
	if err != nil {
		log.Printf("Error copying request to target: %v", err)
		return
	}

	errc := make(chan error, 2)
	cp := func(dst io.Writer, src io.Reader) {
		_, err := io.Copy(dst, src)
		errc <- err
	}
	go cp(d, nc)
	go cp(nc, d)
	<-errc
}

func isWebsocket(h http.Header) bool {
	return strings.EqualFold(h.Get("Connection"), "upgrade") && strings.EqualFold(h.Get("Upgrade"), "websocket")
}
