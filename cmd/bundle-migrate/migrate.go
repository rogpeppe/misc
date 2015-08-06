package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/juju/juju/constraints"
	"github.com/juju/juju/juju/osenv"
	"github.com/juju/utils/parallel"
	"gopkg.in/errgo.v1"
	"gopkg.in/juju/charm.v6-unstable"
	"gopkg.in/juju/charmrepo.v0"
	"gopkg.in/juju/charmrepo.v0/csclient"
	"gopkg.in/juju/charmrepo.v0/migratebundle"
	"gopkg.in/yaml.v2"
)

var verify = flag.Bool("v", false, "verify the bundle after conversion")

func main() {
	flag.Parse()
	bundleName := flag.Arg(0)
	setCacheDir()
	data, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}
	cs := csclient.New(csclient.Params{})
	isSubordinate := isSubordinateFunc(cs)
	bundles, err := migratebundle.Migrate(data, isSubordinate)
	if err != nil {
		log.Fatal(err)
	}
	if len(bundles) != 1 && bundleName == "" {
		var names []string
		for name := range bundles {
			names = append(names, name)
		}
		sort.Strings(names)
		log.Fatal("bundle name argument required (available bundles: %v)", strings.Join(names, " "))
	}
	var bd *charm.BundleData
	if bundleName != "" {
		bd := bundles[bundleName]
		if bd == nil {
			log.Fatal("bundle %q not found in bundle", bundleName)
		}
	} else {
		for _, b := range bundles {
			bd = b
		}
	}
	if !*verify {
		return
	}
	csRepo := charmrepo.NewCharmStore(charmrepo.NewCharmStoreParams{})
	charms, err := fetchCharms(csRepo, bd.RequiredCharms())
	if err != nil {
		log.Fatal(err)
	}
	if err := bd.VerifyWithCharms(verifyConstraint, charms); err != nil {
		verr := err.(*charm.VerificationError)
		fmt.Fprintf(os.Stderr, "verification failed with %d errors\n", len(verr.Errors))
		for _, err := range verr.Errors {
			fmt.Fprintf(os.Stderr, "%s\n", err)
		}
		os.Exit(1)
	}
	data, err = yaml.Marshal(bd)
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(data)
}

func isSubordinateFunc(cs *csclient.Client) func(id *charm.Reference) (bool, error) {
	return func(id *charm.Reference) (bool, error) {
		var meta struct {
			Meta *charm.Meta `csclient:"charm-metadata"`
		}
		if _, err := cs.Meta(id, &meta); err != nil {
			return false, err
		}
		return meta.Meta.Subordinate, nil
	}
}

func fetchCharms(csRepo charmrepo.Interface, ids []string) (map[string]charm.Charm, error) {
	charms := make([]charm.Charm, len(ids))
	run := parallel.NewRun(30)
	for i, id := range ids {
		i, id := i, id
		run.Do(func() error {
			cref, err := charm.ParseReference(id)
			if err != nil {
				return errgo.Notef(err, "bad charm URL %q", id)
			}
			curl, err := csRepo.Resolve(cref)
			if err != nil {
				return errgo.Notef(err, "cannot resolve URL %q", id)
			}
			c, err := csRepo.Get(curl)
			if err != nil {
				return errgo.Notef(err, "cannot get %q", id)
			}
			charms[i] = c
			return nil
		})
	}
	if err := run.Wait(); err != nil {
		return nil, err
	}
	m := make(map[string]charm.Charm)
	for i, id := range ids {
		m[id] = charms[i]
	}
	return m, nil
}

func verifyConstraint(c string) error {
	_, err := constraints.Parse(c)
	return err
}

func setCacheDir() {
	jujuHome := osenv.JujuHomeDir()
	if jujuHome == "" {
		log.Fatal("cannot determine juju home, required environment variables are not set")
	}
	osenv.SetJujuHome(jujuHome)
	charmrepo.CacheDir = osenv.JujuHomePath("charmcache")
}
