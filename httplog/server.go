package httplog

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"sync/atomic"
	"unicode/utf8"

	"github.com/juju/errgo"
)

func Handler(logger Logger, h http.Handler) http.Handler {
	if logger == nil {
		logger = StdlogLogger
	}
	return loggingHandler{
		logger: logger,
		h:      h,
	}
}

type loggingHandler struct {
	logger Logger
	h      http.Handler
}

func (h loggingHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	id := atomic.AddInt64(&seq, 1)
	w1 := &responseWriter{
		w: w,
	}
	h.logger.Log(ServerRecvRequest(logFromRequest(id, req, false)))
	underlying := h.h
	if underlying == nil {
		underlying = http.DefaultServeMux
	}
	h.h.ServeHTTP(w1, req)
	if w1.statusCode == 0 {
		w1.statusCode = http.StatusOK
	}
	logResp := ServerSendResponse{
		Id:         id,
		Method:     req.Method,
		URL:        req.URL.String(),
		StatusCode: w1.statusCode,
		Header:     w1.Header(),
		Hijacked:   w1.hijacked,
	}
	if utf8.Valid(w1.buf.Bytes()) {
		logResp.Body = w1.buf.String()
	} else {
		logResp.Body64 = w1.buf.Bytes()
	}
	h.logger.Log(logResp)
}

type responseWriter struct {
	w          http.ResponseWriter
	buf        bytes.Buffer
	hijacked   bool
	statusCode int
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hw, ok := w.w.(http.Hijacker)
	if !ok {
		return nil, nil, errgo.New("hijacker not implemented")
	}
	w.hijacked = true
	return hw.Hijack()
}

// TODO implement Flusher, Pusher and CloseNotifier.

func (w *responseWriter) Header() http.Header {
	return w.w.Header()
}

func (w *responseWriter) Write(buf []byte) (int, error) {
	// TODO stop writing if there's a large body.
	w.buf.Write(buf)
	return w.w.Write(buf)
}

func (w *responseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	// TODO snapshot the current header?
}
