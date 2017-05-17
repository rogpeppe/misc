package main

import (
	"flag"
	"fmt"
	"sort"
	"strings"
	"sync"

	"gopkg.in/amz.v3/aws"
	"gopkg.in/amz.v3/ec2"
)

func init() {
	flags := flag.NewFlagSet("allinstances", flag.ExitOnError)
	addInstancesFlags(flags)
	cmds = append(cmds, cmd{
		name:           "allinstances",
		runMultiRegion: allInstances,
		flags:          flags,
	})
}

func allInstances(c cmd, args []string) {
	if len(args) != 0 {
		c.usage()
	}
	instances := make(chan instanceResult)
	var wg sync.WaitGroup
	forAllRegions(func(conn *ec2.EC2) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sendInstances(conn, instances); err != nil {
				errorf("could not list instances in region %s: %v (%T)", conn.Region.Name, err, err)
			}
		}()
	})
	go func() {
		wg.Wait()
		close(instances)
	}()
	var allInstances []instanceResult
	for inst := range instances {
		allInstances = append(allInstances, inst)
	}
	sort.Slice(allInstances, func(i, j int) bool {
		inst0, inst1 := &allInstances[i], &allInstances[j]
		if inst0.regionName != inst1.regionName {
			return inst0.regionName < inst1.regionName
		}
		return inst0.InstanceId < inst1.InstanceId
	})
	for _, inst := range allInstances {
		if !instancesFlags.all && inst.State.Name == "terminated" {
			continue
		}
		fmt.Printf("%s %s\n", inst.regionName, inst)
	}
}

type instanceResult struct {
	regionName string
	ec2.Instance
}

func (inst instanceResult) String() string {
	s := inst.InstanceId
	if instancesFlags.state {
		s += " " + inst.State.Name
	}
	if instancesFlags.addr {
		s += " " + inst.DNSName
	}
	return s
}

func sendInstances(conn *ec2.EC2, instances chan<- instanceResult) error {
	resp, err := conn.Instances(nil, nil)
	if err != nil {
		return err
	}
	for _, r := range resp.Reservations {
		for _, inst := range r.Instances {
			instances <- instanceResult{
				regionName: conn.Region.Name,
				Instance:   inst,
			}
		}
	}
	return nil
}

func init() {
	flags := flag.NewFlagSet("allvolumes", flag.ExitOnError)
	addVolumesFlags(flags)
	cmds = append(cmds, cmd{
		name:           "allvolumes",
		runMultiRegion: allVolumes,
		flags:          flags,
	})
}

func allVolumes(c cmd, args []string) {
	volumes := make(chan volumeResult)
	var wg sync.WaitGroup
	forAllRegions(func(conn *ec2.EC2) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sendVolumes(conn, volumes); err != nil {
				errorf("could not list instances in region %s: %v (%T)", conn.Region.Name, err, err)
			}
		}()
	})
	go func() {
		wg.Wait()
		close(volumes)
	}()
	var allVolumes []volumeResult
	for volume := range volumes {
		allVolumes = append(allVolumes, volume)
	}
	sort.Slice(allVolumes, func(i, j int) bool {
		v0, v1 := &allVolumes[i], &allVolumes[j]
		if v0.regionName != v1.regionName {
			return v0.regionName < v1.regionName
		}
		return v0.Id < v1.Id
	})
	for _, v := range allVolumes {
		fmt.Printf("%s %s\n", v.regionName, v)
	}
}

type volumeResult struct {
	regionName string
	ec2.Volume
}

func (v volumeResult) String() string {
	s := v.Id
	if volumesFlags.size {
		s += " " + fmt.Sprint(v.Size)
	}
	if volumesFlags.ctime {
		s += " " + fmt.Sprint(v.CreateTime)
	}
	return s
}

func sendVolumes(conn *ec2.EC2, volumes chan<- volumeResult) error {
	resp, err := conn.Volumes(nil, nil)
	if err != nil {
		return err
	}
	for _, v := range resp.Volumes {
		volumes <- volumeResult{
			regionName: conn.Region.Name,
			Volume:     v,
		}
	}
	return nil
}

func forAllRegions(f func(conn *ec2.EC2)) {
	for _, region := range aws.Regions {
		region := region
		if differentAuthDomain(region.Name) {
			continue
		}
		signer := aws.SignV4Factory(region.Name, "ec2")
		f(ec2.New(awsAuth, region, signer))
	}
}

// differentAuthDomain reports whether the given region
// name lives in a different authentication domain
// to all the other regions.
func differentAuthDomain(name string) bool {
	return strings.HasPrefix(name, "us-gov-") ||
		strings.HasPrefix(name, "cn-")
}
