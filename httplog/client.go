package httplog

import (
	"bytes"
	"io"
	"io/ioutil"
	"net/http"
	"sync/atomic"
	"unicode/utf8"
)

func Transport(logger Logger, t http.RoundTripper) http.RoundTripper {
	if logger == nil {
		logger = StdlogLogger
	}
	if t == nil {
		t = http.DefaultTransport
	}
	return loggingTransport{
		t:      t,
		logger: logger,
	}
}

type loggingTransport struct {
	t      http.RoundTripper
	logger Logger
}

func (t loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	id := atomic.AddInt64(&seq, 1)
	t.logger.Log(logFromRequest(id, req, true))
	underlying := t.t
	if underlying == nil {
		underlying = http.DefaultTransport
	}
	resp, err := underlying.RoundTrip(req)
	if err != nil {
		t.logger.Log(ClientRecvResponse{
			Id:    id,
			Error: err.Error(),
		})
		return nil, err
	}
	logResp := ClientRecvResponse{
		Id:         id,
		Method:     req.Method,
		URL:        req.URL.String(),
		Header:     resp.Header,
		StatusCode: resp.StatusCode,
	}
	logResp.Body, logResp.Body64, logResp.BodyTruncated = replaceBody(&resp.Body, true)
	t.logger.Log(logResp)
	return resp, nil
}

func logFromRequest(id int64, req *http.Request, closeBody bool) ClientSendRequest {
	logReq := ClientSendRequest{
		Id:     id,
		URL:    req.URL.String(),
		Method: req.Method,
		Header: req.Header,
	}
	logReq.Body, logReq.Body64, logReq.BodyTruncated = replaceBody(&req.Body, closeBody)
	return logReq
}

func replaceBody(r *io.ReadCloser, needClose bool) (body string, body64 []byte, truncated bool) {
	if *r == nil {
		return "", nil, false
	}
	data, err := ioutil.ReadAll(*r)
	if err != nil {
		truncated = true
	}
	if needClose {
		(*r).Close()
	}
	// TODO limit the number of bytes read.
	*r = ioutil.NopCloser(bytes.NewReader(data))

	if utf8.Valid(data) {
		return string(data), nil, false
	}
	return "", data, false
}
