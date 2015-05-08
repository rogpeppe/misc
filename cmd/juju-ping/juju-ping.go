package main

import (
	"fmt"
	flag "launchpad.net/gnuflag"
	"os"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/juju/errors"
	"github.com/juju/juju/cmd/envcmd"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/configstore"
	"github.com/juju/juju/juju"
	"github.com/juju/juju/juju/osenv"
	"github.com/juju/juju/state/api"
	"github.com/juju/names"
)

var (
	envName = flag.String("e", "", "environment name")
	verbose = flag.Bool("v", false, "verbose")
	timeout = flag.Duration("timeout", 5*time.Minute, "maximum timeout")
)

func main() {
	flag.Parse(true)
	if err := main0(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

var startTime = time.Now()

func main0() error {
	if err := juju.InitJujuHome(); err != nil {
		return fmt.Errorf("cannot initialise juju home: %v", err)
	}
	store, err := configstore.Default()
	if err != nil {
		return err
	}
	envName, err := getDefaultEnvironment()
	if err != nil {
		return err
	}
	info, err := store.ReadInfo(envName)
	if err != nil {
		return err
	}
	ep := info.APIEndpoint()
	creds := info.APICredentials()
	donec := make(chan pingTimes)
	sort.Strings(ep.Addresses)
	prevAddr := ""
	count := 0
	need := make(map[string]bool)
	for _, addr := range ep.Addresses {
		if addr == prevAddr {
			continue
		}
		addr := addr
		need[addr] = true
		apiInfo := &api.Info{
			Addrs:      []string{addr},
			CACert:     ep.CACert,
			Tag:        names.NewUserTag(creds.User).String(),
			Password:   creds.Password,
			EnvironTag: names.NewEnvironTag(ep.EnvironUUID).String(),
		}
		go func() {
			donec <- ping(apiInfo)
		}()
		count++
		prevAddr = addr
	}
	max := -1
	for _, addr := range ep.Addresses {
		if n := utf8.RuneCountInString(addr); n > max {
			max = n
		}
	}
	expirec := time.After(*timeout)
	for i := 0; i < count; i++ {
		select {
		case times := <-donec:
			fmt.Printf("%*s %s %s\n", -max, times.addr, formatDuration(times.open), formatDuration(times.login))
			delete(need, times.addr)
		case <-expirec:
			printf("timed out after %v", *timeout)
			for addr := range need {
				printf("no reply from %s", addr)
			}
			os.Exit(2)
		}
	}
	printf("all done\n")
	return nil
}

type pingTimes struct {
	addr  string
	open  time.Duration
	login time.Duration
}

func ping(apiInfo *api.Info) pingTimes {
	logf := func(f string, a ...interface{}) {
		printf("%s: %s", apiInfo.Addrs[0], fmt.Sprintf(f, a...))
	}
	times, err := ping0(apiInfo, logf)
	if err != nil {
		logf("%v", err)
	}
	return times
}

var dialOpts = api.DialOpts{
	// DialAddressInterval is irrelevant because we
	// never use more than one address
	Timeout:    20 * time.Second,
	RetryDelay: 2 * time.Second,
}

func ping0(apiInfo *api.Info, logf func(string, ...interface{})) (pingTimes, error) {
	var times = pingTimes{
		addr:  apiInfo.Addrs[0],
		open:  -1,
		login: -1,
	}
	t0 := time.Now()
	tag, password := apiInfo.Tag, apiInfo.Password
	apiInfo.Tag = ""
	apiInfo.Password = ""
	logf("start")
	st, err := api.Open(apiInfo, dialOpts)
	if err != nil {
		return times, err
	}
	times.open = time.Since(t0)
	defer st.Close()
	logf("opened API")
	if err := st.Login(tag, password, ""); err != nil {
		return times, fmt.Errorf("cannot log in: %v", err)
	}
	times.login = time.Since(t0) - times.open
	logf("logged in")
	return times, nil
}

// getDefaultEnvironment is copied from github.com/juju/juju/cmd/envcmd/environmentcommand.go
func getDefaultEnvironment() (string, error) {
	if defaultEnv := os.Getenv(osenv.JujuEnvEnvKey); defaultEnv != "" {
		return defaultEnv, nil
	}
	if currentEnv := envcmd.ReadCurrentEnvironment(); currentEnv != "" {
		return currentEnv, nil
	}
	envs, err := environs.ReadEnvirons("")
	if environs.IsNoEnv(err) {
		// That's fine, not an error here.
		return "", nil
	}
	if err != nil {
		return "", errors.Trace(err)
	}
	return envs.Default, nil
}

func printf(f string, a ...interface{}) {
	if !*verbose {
		return
	}
	d := time.Since(startTime)
	msec := d / time.Millisecond
	sec := d / time.Second
	min := d / time.Minute
	fmt.Printf("%d:%02d.%03d %s\n", min, sec%60, msec%1000, fmt.Sprintf(f, a...))
}

func formatDuration(d time.Duration) string {
	if d == -1 {
		return "          "
	}
	return fmt.Sprintf("%10d", d/time.Millisecond)
}
