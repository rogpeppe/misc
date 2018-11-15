package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/juju/errors"
	"github.com/juju/juju/jujuclient"
	"github.com/juju/loggo"
	cookiejar "github.com/juju/persistent-cookiejar"
	"github.com/rogpeppe/misc/jujuconn"
	errgo "gopkg.in/errgo.v1"
	"gopkg.in/macaroon-bakery.v2-unstable/bakery"
	"gopkg.in/macaroon-bakery.v2-unstable/httpbakery"
	"gopkg.in/macaroon-bakery.v2-unstable/httpbakery/agent"
	bakeryV2 "gopkg.in/macaroon-bakery.v2/bakery"
	agentV2 "gopkg.in/macaroon-bakery.v2/httpbakery/agent"
)

var debug = flag.Bool("debug", false, "print debug messages")

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: juju-auth <controller>[:<model>]\n")
		fmt.Fprintf(os.Stderr, "Set BAKERY_AGENT_FILE to a path to the agent auth file\n")
		os.Exit(2)
	}
	flag.Parse()
	if flag.NArg() > 1 {
		flag.Usage()
	}
	if *debug {
		loggo.ConfigureLoggers("DEBUG")
	}
	controller := flag.Arg(0)
	model := ""
	cm := strings.SplitN(controller, ":", 2)
	if len(cm) > 1 {
		controller, model = cm[0], cm[1]
	}
	if controller == "" {
		log.Fatalf("controller must be non-empty")
	}
	if err := jujuAuth(controller, model); err != nil {
		fmt.Fprint(os.Stderr, "%v\n", err)
	}
	fmt.Println("authenticated successfully")
}

func jujuAuth(controller, model string) error {
	if err := jujuconn.Init(); err != nil {
		return errgo.Mask(err)
	}

	authInfo, err := agentV2.AuthInfoFromEnvironment()
	if err != nil {
		return errgo.Mask(err)
	}
	jarPath := jujuclient.JujuCookiePath(controller)
	// Remove all cookies so we'll definitely refresh them.
	os.Remove(jarPath)
	jar, err := cookiejar.New(&cookiejar.Options{
		Filename: jarPath,
	})
	if err != nil {
		return errors.Trace(err)
	}
	defer func() {
		if err := jar.Save(); err != nil {
			log.Printf("cannot save cookie jar: %v", err)
		}
		fmt.Printf("saved authentication credentials to %s\n", jarPath)
	}()
	bclient := httpbakery.NewClient()
	bclient.Jar = jar
	bclient.Key = fromBakeryV2Key(authInfo.Key)
	for _, a := range authInfo.Agents {
		u, err := url.Parse(a.URL)
		if err != nil {
			return errgo.Notef(err, "invalid controller URL %q", a.URL)
		}
		if err := agent.SetUpAuth(bclient, u, a.Username); err != nil {
			return errgo.Mask(err)
		}
	}
	ctxt, err := jujuconn.NewContextWithParams(jujuconn.Params{
		BakeryClient: bclient,
	})
	if err != nil {
		return errgo.Mask(err)
	}
	if _, err := ctxt.DialModel("", ""); err != nil {
		return errgo.Notef(err, "cannot dial model")
	}
	return nil
}

func fromBakeryV2Key(k2 *bakeryV2.KeyPair) *bakery.KeyPair {
	var k bakery.KeyPair
	copy(k.Public.Key[:], k2.Public.Key[:])
	copy(k.Private.Key[:], k2.Private.Key[:])
	return &k
}
