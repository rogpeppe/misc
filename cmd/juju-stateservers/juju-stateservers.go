package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/mongo"
	"github.com/juju/juju/state"
	"github.com/juju/replicaset"
	"github.com/kr/pretty"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/mgo.v2/txn"
	flag "launchpad.net/gnuflag"
)

var nflag = flag.Bool("n", false, "print ops but don't actually change anything")

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: juju-stateservers machine-id...\n")
		flag.PrintDefaults()
		os.Exit(2)
	}
	flag.Parse(true)
	if err := setStateServers(flag.Args()); err != nil {
		fmt.Fprintf(os.Stderr, "cannot set state servers: %v\n", err)
		os.Exit(1)
	}
}

type serverInfo struct {
	servers  *stateServersDoc
	machines map[string]*state.Machine
}

type stateServersDoc struct {
	Id               string `bson:"_id"`
	MachineIds       []string
	VotingMachineIds []string

	TxnRevno int64 `bson:"txn-revno"`
}

func setStateServers(ids []string) error {
	st, db, runner, err := openState()
	if err != nil {
		return err
	}
	if err := printReplicasetMembers(db.Session); err != nil {
		return err
	}
	return setStateServers0(ids, st, db, runner)
}

func setStateServers0(ids []string, st *state.State, db *mgo.Database, runner *txn.Runner) error {
	if len(ids)%2 == 0 || len(ids) == 0 {
		return fmt.Errorf("number of state servers must be odd and greater than zero")
	}
	info, err := currentInfo(st, db)
	if err != nil {
		return err
	}
	fmt.Printf("server ids: %v\n", info.servers.MachineIds)
	fmt.Printf("voting: %v\n", info.servers.VotingMachineIds)
	for _, m := range info.machines {
		fmt.Printf("machine-%s jobs %s; wantsvote %v; hasvote %v\n", m.Id(), m.Jobs(), m.WantsVote(), m.HasVote())
	}
	if len(info.machines) != len(info.servers.MachineIds) {
		fmt.Printf("warning: stateservers.MachineIds does not hold all state servers\n")
	}
	wantsVote := make(map[string]bool)
	for _, id := range ids {
		if _, ok := info.machines[id]; !ok {
			return fmt.Errorf("machine %s is not in current server list", id)
		}
		wantsVote[id] = true
	}
	ops := []txn.Op{{
		C:      "stateServers",
		Id:     "e",
		Assert: bson.D{{"txn-revno", info.servers.TxnRevno}},
		Update: bson.D{{"$set", bson.D{{"votingmachineids", ids}}}},
	}}
	for id := range info.machines {
		ops = append(ops, txn.Op{
			C:      "machines",
			Id:     id,
			Update: bson.D{{"$set", bson.D{{"novote", !wantsVote[id]}}}},
		})
	}
	if *nflag {
		pretty.Println(ops)
	} else {
		err := runner.Run(ops, "", nil)
		if err != nil {
			return fmt.Errorf("cannot execute transaction: %v", err)
		}
	}
	return nil
}

func printReplicasetMembers(session *mgo.Session) error {
	members, err := replicaset.CurrentMembers(session)
	if err != nil {
		return fmt.Errorf("cannot get replica set members: %v", err)
	}
	statusResult, err := replicaset.CurrentStatus(session)
	if err != nil {
		return fmt.Errorf("cannot get replica set status: %v", err)
	}
	statuses := make(map[int]*replicaset.MemberStatus)
	for i, status := range statusResult.Members {
		statuses[status.Id] = &statusResult.Members[i]
	}
	for _, m := range members {
		votes := 1
		if m.Votes != nil {
			votes = *m.Votes
		}
		status := statuses[m.Id]
		if status == nil {
			fmt.Printf("id %3v has no replica set status\n", m.Id)
		}
		fmt.Printf("id %3v; machine id %5q; address %50s; votes %v; healthy %v; state %v\n", m.Id, m.Tags["juju-machine-id"], m.Address, votes, status.Healthy, status.State)
	}
	return nil
}

func openState() (*state.State, *mgo.Database, *txn.Runner, error) {
	stInfo, err := stateInfo()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("cannot get state info: %v", err)
	}
	st, err := state.Open(stInfo, mongo.DialOpts{
		Timeout: 20 * time.Second,
	}, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("cannot open state: %v", err)
	}
	db := st.MongoSession().DB("juju")
	runner := txn.NewRunner(db.C("txns"))
	return st, db, runner, nil
}

func currentInfo(st *state.State, db *mgo.Database) (*serverInfo, error) {
	var doc stateServersDoc
	err := db.C("stateServers").Find(bson.D{{"_id", "e"}}).One(&doc)
	if err != nil {
		return nil, fmt.Errorf("cannot get state server info: %v", err)
	}
	ms := make(map[string]*state.Machine)
	var all []string
	all = append(all, doc.MachineIds...)
	all = append(all, doc.VotingMachineIds...)
	for _, id := range all {
		if _, ok := ms[id]; ok {
			continue
		}
		m, err := st.Machine(id)
		if err != nil {
			return nil, fmt.Errorf("cannot get info on machine %s: %v", id, err)
		}
		ms[id] = m
	}
	return &serverInfo{
		servers:  &doc,
		machines: ms,
	}, nil
}

type mongoCreds struct {
	username string
	password string
	caCert   string
}

func stateInfo() (*state.Info, error) {
	dataDir := agent.DefaultDataDir
	tag, err := machineAgentTag(dataDir)
	if err != nil {
		return nil, err
	}
	cfgPath := agent.ConfigPath(dataDir, tag)
	cfg, err := agent.ReadConfig(cfgPath)
	if err != nil {
		return nil, err
	}
	info, ok := cfg.StateInfo()
	if !ok {
		return nil, fmt.Errorf("no state info found")
	}
	return info, nil
}

func machineAgentTag(dataDir string) (string, error) {
	entries, err := ioutil.ReadDir(filepath.Join(dataDir, "agents"))
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "machine-") {
			return e.Name(), nil
		}
	}
	return "", fmt.Errorf("no machine agent entry found")
}
