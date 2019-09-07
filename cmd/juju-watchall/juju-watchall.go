package main

import (
	stdjson "encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"reflect"
	"strings"

	"github.com/juju/juju/api"
	apicontroller "github.com/juju/juju/api/controller"
	"github.com/juju/juju/state/multiwatcher"
	"github.com/juju/loggo"
	"github.com/rogpeppe/misc/jujuconn"
	rjson "github.com/rogpeppe/rjson"
)

var (
	allFlag   = flag.Bool("a", false, "watch all models")
	jsonFlag  = flag.Bool("j", false, "print JSON not RJSON output")
	debugFlag = flag.Bool("debug", false, "run in debug mode")
)

var json = struct {
	MarshalIndent func(v interface{}, prefix, indent string) ([]byte, error)
	Marshal       func(v interface{}) ([]byte, error)
	Unmarshal     func([]byte, interface{}) error
}{
	MarshalIndent: rjson.MarshalIndent,
	Marshal:       rjson.Marshal,
	Unmarshal:     rjson.Unmarshal,
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "juju-watchall [flags] [<controller>|<model>|[<controller>]:[<model>]]\n")
		fmt.Fprintf(os.Stderr, `
juju-watchall modelname
	- watches the named model on the current controller.
juju-watchall
	- watches the current model on the current controller
juju-watchall controllername:modelname
	- watches the named model on the named controller
juju-watchall -a controllername
	- watches all models on the named controller.
juju-watchall -a
	- watches all models on the current controller.
`)
		os.Exit(2)
	}
	flag.Parse()
	if *jsonFlag {
		json.MarshalIndent = stdjson.MarshalIndent
		json.Marshal = stdjson.Marshal
		json.Unmarshal = stdjson.Unmarshal
	}
	if *debugFlag {
		loggo.ConfigureLoggers("TRACE")
	}
	controllerName, modelName := "", ""
	if flag.NArg() > 0 {
		split := strings.Split(flag.Arg(0), ":")
		switch len(split) {
		case 1:
			if *allFlag {
				controllerName = split[0]
			} else {
				modelName = split[0]
			}
		case 2:
			controllerName, modelName = split[0], split[1]
		default:
			flag.Usage()
		}
	}
	var w *api.AllWatcher
	if *allFlag {
		conn, err := jujuconn.DialController(controllerName)
		if err != nil {
			log.Fatalf("cannot dial controller: %v", err)
		}
		w, err = apicontroller.NewClient(conn).WatchAllModels()
		if err != nil {
			log.Fatalf("cannot watch all models: %v", err)
		}
	} else {
		conn, err := jujuconn.DialModel(controllerName, modelName)
		if err != nil {
			log.Fatalf("cannot dial model: %v", err)
		}
		w, err = conn.Client().WatchAll()
		if err != nil {
			log.Fatalf("cannot watch models: %v", err)
		}
	}
	entities := make(map[multiwatcher.EntityId]map[string]interface{})
	for {
		deltas, err := w.Next()
		if err != nil {
			log.Fatalf("Next error: %v", err)
		}
		for _, d := range deltas {
			id := d.Entity.EntityId()
			if d.Removed {
				fmt.Printf("- %s %v\n", id.Kind, id.Id)
				delete(entities, id)
				continue
			}
			oldFields := entities[id]
			if oldFields == nil {
				data, _ := json.MarshalIndent(d.Entity, "", "\t")
				var fields map[string]interface{}
				if err := json.Unmarshal(data, &fields); err != nil {
					panic("cannot re-unmrshal json")
				}
				entities[id] = fields
				fmt.Printf("+ %s %v %v %s\n", id.Kind, id.Id, id.ModelUUID, data)
				continue
			}
			data, _ := json.Marshal(d.Entity)
			var fields map[string]interface{}
			if err := json.Unmarshal(data, &fields); err != nil {
				panic("cannot re-unmrshal json")
			}
			names := make(map[string]bool)
			for name := range oldFields {
				names[name] = true
			}
			for name := range fields {
				names[name] = true
			}
			changedFields := make(map[string]interface{})
			for name := range names {
				if !reflect.DeepEqual(fields[name], oldFields[name]) {
					changedFields[name] = fields[name]
				}
			}
			entities[id] = fields
			data, _ = json.MarshalIndent(changedFields, "", "\t")
			fmt.Printf("| %s %v %v %s\n", id.Kind, id.Id, id.ModelUUID, data)
		}
		fmt.Printf("\n")
	}
}
