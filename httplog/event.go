package httplog

import "net/http"

type EventKind string

const (
	KindClientSendRequest  EventKind = "client ->"
	KindClientRecvResponse EventKind = "client <-"
	KindServerRecvRequest  EventKind = "server <-"
	KindServerSendResponse EventKind = "server ->"
)

var seq int64

type Event interface {
	Kind() EventKind
}

type ClientSendRequest struct {
	Id            int64       `json:"id"`
	Method        string      `json:"method"`
	URL           string      `json:"url"`
	Header        http.Header `json:"header"`
	Body          string      `json:"body,omitempty"`
	Body64        []byte      `json:"body64,omitempty"`
	BodyTruncated bool        `json:"bodyTruncated,omitempty"`
	BodyFlushed   bool        `json:"bodyFlushed,omitempty"`
	Hijacked      bool        `json:"hijacked,omitempty"`
}

func (e ClientSendRequest) Kind() EventKind {
	return KindClientSendRequest
}

type ClientRecvResponse struct {
	Id            int64       `json:"id"`
	Method        string      `json:"method,omitempty"`
	URL           string      `json:"url,omitempty"`
	Error         string      `json:"error,omitempty"`
	StatusCode    int         `json:"statusCode,omitempty"`
	Header        http.Header `json:"header,omitempty"`
	Body          string      `json:"body,omitempty"`
	Body64        []byte      `json:"body64,omitempty"`
	BodyTruncated bool        `json:"bodyTruncated,omitempty"`
	Hijacked bool	`json:"hijacked,omitempty"`
}

func (e ClientRecvResponse) Kind() EventKind {
	return KindClientRecvResponse
}

type ServerRecvRequest ClientSendRequest

func (e ServerRecvRequest) Kind() EventKind {
	return KindServerRecvRequest
}

type ServerSendResponse ClientRecvResponse

func (e ServerSendResponse) Kind() EventKind {
	return KindServerSendResponse
}
