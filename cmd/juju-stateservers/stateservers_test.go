package main

import (
	"github.com/juju/juju/constraints"
	"github.com/juju/juju/juju/testing"
	"gopkg.in/mgo.v2/txn"
	gc "launchpad.net/gocheck"
)

type suite struct {
	testing.JujuConnSuite
}

var _ = gc.Suite(&suite{})

func (s *suite) TestSetStateServers(c *gc.C) {
	changes, err := s.State.EnsureAvailability(5, constraints.Value{}, "precise")
	c.Assert(err, gc.IsNil)
	c.Assert(changes.Added, gc.HasLen, 5)

	session := s.State.MongoSession()
	db := session.DB("juju")
	runner := txn.NewRunner(db.C("txns"))
	err = setStateServers0([]string{"0", "1", "4"}, s.State, db, runner)
	c.Assert(err, gc.IsNil)
}
