package httplog

import (
	"encoding/json"
	"log"
)

type Logger interface {
	Log(event Event)
}

var StdlogLogger stdlogLogger

type stdlogLogger struct{}

func (stdlogLogger) Log(event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		panic(err)
	}
	log.Printf("%s %s", event.Kind(), data)
}
