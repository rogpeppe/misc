package main

import (
	"bytes"
	"flag"
	"fmt"
	"gopkg.in/amz.v3/aws"
	"gopkg.in/amz.v3/ec2"
	"gopkg.in/errgo.v1"
	"os"
	"regexp"
	"strings"

	"github.com/juju/utils/parallel"
)

type cmd struct {
	name           string
	args           string
	run            func(cmd, *ec2.EC2, []string)
	runMultiRegion func(cmd, []string)
	flags          *flag.FlagSet
}

var cmds []cmd

var awsAuth aws.Auth

func main() {
	flag.Parse()
	if flag.Arg(0) == "" {
		errorf("no command")
		os.Exit(2)
	}
	var regionName string
	for i := range cmds {
		c := &cmds[i]
		if c.flags == nil {
			c.flags = flag.NewFlagSet(c.name, flag.ExitOnError)
		}
		if c.runMultiRegion == nil {
			c.flags.StringVar(&regionName, "region", aws.USEast.Name, "AWS region")
		}
	}
	if flag.Arg(0) == "help" {
		for _, c := range cmds {
			c.printUsage()
		}
		return
	}
	var found cmd
	for _, c := range cmds {
		if flag.Arg(0) == c.name {
			found = c
			break
		}
	}
	if found.name == "" {
		errorf("unknown command %q", flag.Arg(0))
		os.Exit(2)
	}
	auth, err := aws.EnvAuth()
	if err != nil {
		fatalf("envauth: %v", err)
	}
	awsAuth = auth
	if found.flags == nil {
		found.flags = flag.NewFlagSet(found.name, flag.ExitOnError)
	}
	found.flags.Parse(flag.Args()[1:])
	if found.runMultiRegion != nil {
		found.runMultiRegion(found, found.flags.Args())
		return
	}
	region, ok := aws.Regions[regionName]
	if !ok {
		fatalf("no such region")
	}
	signer := aws.SignV4Factory(region.Name, "ec2")
	conn := ec2.New(auth, region, signer)
	found.run(found, conn, found.flags.Args())
}

func (c cmd) usage() {
	c.printUsage()
	os.Exit(2)
}

func (c cmd) printUsage() {
	errorf("%s %s", c.name, c.args)
	c.flags.PrintDefaults()
}

var groupsFlags struct {
	v   bool
	vv  bool
	ids bool
}

func init() {
	flags := flag.NewFlagSet("groups", flag.ExitOnError)
	flags.BoolVar(&groupsFlags.v, "v", false, "print name, id, owner and description of group")
	flags.BoolVar(&groupsFlags.vv, "vv", false, "print all attributes of group")
	flags.BoolVar(&groupsFlags.ids, "ids", false, "print group ids")
	cmds = append(cmds, cmd{
		name:  "groups",
		run:   groups,
		flags: flags,
	})
}

func groups(c cmd, conn *ec2.EC2, _ []string) {
	resp, err := conn.SecurityGroups(nil, nil)
	check(err, "list groups")
	var b bytes.Buffer
	printf := func(f string, a ...interface{}) {
		fmt.Fprintf(&b, f, a...)
	}
	for _, g := range resp.Groups {
		switch {
		case groupsFlags.vv:
			printf("%s:%s %s %q\n", g.OwnerId, g.Name, g.Id, g.Description)
			for _, p := range g.IPPerms {
				printf("\t")
				printf("\t-proto %s -from %d -to %d", p.Protocol, p.FromPort, p.ToPort)
				for _, g := range p.SourceGroups {
					printf(" %s", g.Id)
				}
				for _, ip := range p.SourceIPs {
					printf(" %s", ip)
				}
				printf("\n")
			}
		case groupsFlags.v:
			printf("%s %s %q\n", g.Name, g.Id, g.Description)
		case groupsFlags.ids:
			printf("%s\n", g.Id)
		default:
			printf("%s\n", g.Name)
		}
	}
	os.Stdout.Write(b.Bytes())
}

func init() {
	flags := flag.NewFlagSet("instances", flag.ExitOnError)
	addInstancesFlags(flags)
	cmds = append(cmds, cmd{
		name:  "instances",
		run:   instances,
		flags: flags,
	})
}

func instances(c cmd, conn *ec2.EC2, args []string) {
	resp, err := conn.Instances(nil, nil)
	if err != nil {
		fatalf("cannot get instances: %v", err)
	}
	var line []string
	for _, r := range resp.Reservations {
		for _, inst := range r.Instances {
			if !instancesFlags.all && inst.State.Name == "terminated" {
				continue
			}
			line = append(line[:0], inst.InstanceId)
			if instancesFlags.state {
				line = append(line, inst.State.Name)
			}
			if instancesFlags.addr {
				if inst.DNSName == "" {
					inst.DNSName = "none"
				}
				line = append(line, inst.DNSName)
			}
			fmt.Printf("%s\n", strings.Join(line, " "))
		}
	}
}

func init() {
	cmds = append(cmds, cmd{
		name: "terminate",
		args: "[instance-id ...]",
		run:  terminate,
	})
}

func terminate(c cmd, conn *ec2.EC2, args []string) {
	if len(args) == 0 {
		return
	}
	_, err := conn.TerminateInstances(args)
	if err != nil {
		fatalf("cannot terminate instances: %v", err)
	}
}

func init() {
	cmds = append(cmds, cmd{
		name: "delgroup",
		args: "[group ...]",
		run:  delgroup,
	})
}

func delgroup(c cmd, conn *ec2.EC2, args []string) {
	run := parallel.NewRun(40)
	for _, g := range args {
		g := g
		run.Do(func() error {
			var ec2g ec2.SecurityGroup
			if secGroupPat.MatchString(g) {
				ec2g.Id = g
			} else {
				ec2g.Name = g
			}
			_, err := conn.DeleteSecurityGroup(ec2g)
			if err != nil {
				errorf("cannot delete %q: %v", g, err)
				return errgo.Newf("error")
			}
			return nil
		})
	}
	if run.Wait() != nil {
		os.Exit(1)
	}
}

func init() {
	flags := flag.NewFlagSet("auth", flag.ExitOnError)
	addIPPermsFlags(flags)
	cmds = append(cmds, cmd{
		name:  "auth",
		args:  "group (sourcegroup|ipaddr)...",
		run:   auth,
		flags: flags,
	})
}

func auth(c cmd, conn *ec2.EC2, args []string) {
	if len(args) < 1 {
		c.usage()
	}
	_, err := conn.AuthorizeSecurityGroup(parseGroup(args[0]), ipPerms(args[1:]))
	check(err, "authorizeSecurityGroup")
}

func parseGroup(s string) ec2.SecurityGroup {
	var g ec2.SecurityGroup
	if secGroupPat.MatchString(s) {
		g.Id = s
	} else {
		g.Name = s
	}
	return g
}

func init() {
	flags := flag.NewFlagSet("revoke", flag.ExitOnError)
	addIPPermsFlags(flags)
	cmds = append(cmds, cmd{
		name:  "revoke",
		args:  "group (sourcegroup|ipaddr)...",
		run:   revoke,
		flags: flags,
	})
}

func revoke(c cmd, conn *ec2.EC2, args []string) {
	if len(args) < 1 {
		c.usage()
	}
	_, err := conn.RevokeSecurityGroup(parseGroup(args[0]), ipPerms(args[1:]))
	check(err, "revokeSecurityGroup")
}

func init() {
	cmds = append(cmds, cmd{
		name: "mkgroup",
		args: "name description",
		run:  mkgroup,
	})
}

func mkgroup(c cmd, conn *ec2.EC2, args []string) {
	if len(args) != 2 {
		c.usage()
	}
	_, err := conn.CreateSecurityGroup("", args[0], args[1])
	check(err, "create security group")
}

func init() {
	cmds = append(cmds, cmd{
		name: "delvolume",
		args: "[volume-id...]",
		run:  delVolume,
	})
}

func delVolume(c cmd, conn *ec2.EC2, args []string) {
	run := parallel.NewRun(40)
	for _, v := range args {
		v := v
		run.Do(func() error {
			_, err := conn.DeleteVolume(v)
			if err != nil {
				errorf("cannot delete %q: %v", v, err)
				return errgo.Newf("error")
			}
			return nil
		})
	}
	if run.Wait() != nil {
		os.Exit(1)
	}
}

var instancesFlags struct {
	addr  bool
	state bool
	all   bool
}

func addInstancesFlags(flags *flag.FlagSet) {
	flags.BoolVar(&instancesFlags.all, "a", false, "print terminated instances too")
	flags.BoolVar(&instancesFlags.addr, "addr", false, "print instance address")
	flags.BoolVar(&instancesFlags.state, "state", false, "print instance state")
}

var ippermsFlags struct {
	fromPort int
	toPort   int
	protocol string
}

func addIPPermsFlags(flags *flag.FlagSet) {
	flags.IntVar(&ippermsFlags.fromPort, "from", 0, "low end of port range")
	flags.IntVar(&ippermsFlags.toPort, "to", 65535, "high end of port range")
	flags.StringVar(&ippermsFlags.protocol, "proto", "tcp", "high end of port range")
}

var volumesFlags struct {
	size  bool
	ctime bool
}

func addVolumesFlags(flags *flag.FlagSet) {
	flags.BoolVar(&volumesFlags.size, "size", false, "show size of volume")
	flags.BoolVar(&volumesFlags.ctime, "ctime", false, "show creation time of volume")
}

var secGroupPat = regexp.MustCompile(`^sg-[a-z0-9]+$`)
var ipPat = regexp.MustCompile(`^[0-9']+\.[0-9]+\.[0-9]+\.[0-9]+/[0-9]+$`)
var groupNamePat = regexp.MustCompile(`^([0-9]+):(.*)$`)

func ipPerms(args []string) (perms []ec2.IPPerm) {
	if len(args) == 0 {
		fatalf("no security groups or ip addresses given")
	}
	var groups []ec2.UserSecurityGroup
	var ips []string
	for _, a := range args {
		switch {
		case ipPat.MatchString(a):
			ips = append(ips, a)
		case secGroupPat.MatchString(a):
			groups = append(groups, ec2.UserSecurityGroup{Id: a})
		case groupNamePat.MatchString(a):
			m := groupNamePat.FindStringSubmatch(a)
			groups = append(groups, ec2.UserSecurityGroup{
				OwnerId: m[1],
				Name:    m[2],
			})
		default:
			fatalf("%q is neither security group id nor ip address", a)
		}
	}
	return []ec2.IPPerm{{
		FromPort:     ippermsFlags.fromPort,
		ToPort:       ippermsFlags.toPort,
		Protocol:     ippermsFlags.protocol,
		SourceGroups: groups,
		SourceIPs:    ips,
	}}
	return
}

func check(err error, e string, a ...interface{}) {
	if err == nil {
		return
	}
	fatalf("%s: %v", fmt.Sprintf(e, a...), err)
}

func errorf(f string, args ...interface{}) {
	if !strings.HasSuffix(f, "\n") {
		f += "\n"
	}
	f = "ec2: " + f
	fmt.Fprintf(os.Stderr, f, args...)
}

func fatalf(f string, args ...interface{}) {
	errorf(f, args...)
	os.Exit(2)
}
