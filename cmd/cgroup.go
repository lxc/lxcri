package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

// https://github.com/opencontainers/runtime-spec/blob/v1.0.2/config-linux.md
// TODO New spec will contain a property Unified for cgroupv2 properties
// https://github.com/opencontainers/runtime-spec/blob/master/config-linux.md#unified
func configureCgroup(spec *specs.Spec) error {
	if err := configureCgroupPath(spec.Linux); err != nil {
		return errors.Wrap(err, "failed to configure cgroup path")
	}

	// lxc.cgroup.root and lxc.cgroup.relative must not be set for cgroup v2
	if err := clxc.setConfigItem("lxc.cgroup.relative", "0"); err != nil {
		return err
	}

	if devices := spec.Linux.Resources.Devices; devices != nil {
		if err := configureDeviceController(spec); err != nil {
			return err
		}
	}

	if mem := spec.Linux.Resources.Memory; mem != nil {
		log.Debug().Msg("TODO cgroup memory controller not implemented")
	}

	if cpu := spec.Linux.Resources.CPU; cpu != nil {
		if err := configureCPUController(cpu); err != nil {
			return err
		}
	}

	if pids := spec.Linux.Resources.Pids; pids != nil {
		if err := clxc.setConfigItem("lxc.cgroup2.pids.max", fmt.Sprintf("%d", pids.Limit)); err != nil {
			return err
		}
	}
	if blockio := spec.Linux.Resources.BlockIO; blockio != nil {
		log.Debug().Msg("TODO cgroup blockio controller not implemented")
	}

	if hugetlb := spec.Linux.Resources.HugepageLimits; hugetlb != nil {
		// set Hugetlb limit (in bytes)
		log.Debug().Msg("TODO cgroup hugetlb controller not implemented")
	}
	if net := spec.Linux.Resources.Network; net != nil {
		log.Debug().Msg("TODO cgroup network controller not implemented")
	}
	return nil
}

func configureCgroupPath(linux *specs.Linux) error {
	if linux.CgroupsPath == "" {
		return fmt.Errorf("empty cgroups path in spec")
	}
	if !clxc.SystemdCgroup {
		return clxc.setConfigItem("lxc.cgroup.dir", linux.CgroupsPath)
	}
	cgPath := parseSystemdCgroupPath(linux.CgroupsPath)
	// @since lxc @a900cbaf257c6a7ee9aa73b09c6d3397581d38fb
	// checking for on of the config items shuld be enough, because they were introduced together ...
	if supportsConfigItem("lxc.cgroup.dir.container", "lxc.cgroup.dir.monitor") {
		if err := clxc.setConfigItem("lxc.cgroup.dir.container", cgPath.String()); err != nil {
			return err
		}
		if err := clxc.setConfigItem("lxc.cgroup.dir.monitor", filepath.Join(clxc.MonitorCgroup, clxc.Container.Name()+".scope")); err != nil {
			return err
		}
	} else {
		if err := clxc.setConfigItem("lxc.cgroup.dir", cgPath.String()); err != nil {
			return err
		}
	}
	if supportsConfigItem("lxc.cgroup.dir.monitor.pivot") {
		if err := clxc.setConfigItem("lxc.cgroup.dir.monitor.pivot", clxc.MonitorCgroup); err != nil {
			return err
		}
	}
	return nil
}

func configureDeviceController(spec *specs.Spec) error {
	devicesAllow := "lxc.cgroup2.devices.allow"
	devicesDeny := "lxc.cgroup2.devices.deny"

	if !clxc.CgroupDevices {
		log.Warn().Msg("cgroup device controller is disabled (access to all devices is granted)")
		// allow read-write-mknod access to all char and block devices
		if err := clxc.setConfigItem(devicesAllow, "b *:* rwm"); err != nil {
			return err
		}
		if err := clxc.setConfigItem(devicesAllow, "c *:* rwm"); err != nil {
			return err
		}
		return nil
	}

	// Set cgroup device permissions from spec.
	// Device rule parsing in LXC is not well documented in lxc.container.conf
	// see https://github.com/lxc/lxc/blob/79c66a2af36ee8e967c5260428f8cdb5c82efa94/src/lxc/cgroups/cgfsng.c#L2545
	// Mixing allow/deny is not permitted by lxc.cgroup2.devices.
	// Best practise is to build up an allow list to disable access restrict access to new/unhandled devices.

	anyDevice := ""
	blockDevice := "b"
	charDevice := "c"

	for _, dev := range spec.Linux.Resources.Devices {
		key := devicesDeny
		if dev.Allow {
			key = devicesAllow
		}

		maj := "*"
		if dev.Major != nil {
			maj = fmt.Sprintf("%d", *dev.Major)
		}

		min := "*"
		if dev.Minor != nil {
			min = fmt.Sprintf("%d", *dev.Minor)
		}

		switch dev.Type {
		case anyDevice:
			// do not deny any device, this will also deny access to default devices
			if !dev.Allow {
				continue
			}
			// decompose
			val := fmt.Sprintf("%s %s:%s %s", blockDevice, maj, min, dev.Access)
			if err := clxc.setConfigItem(key, val); err != nil {
				return err
			}
			val = fmt.Sprintf("%s %s:%s %s", charDevice, maj, min, dev.Access)
			if err := clxc.setConfigItem(key, val); err != nil {
				return err
			}
		case blockDevice, charDevice:
			val := fmt.Sprintf("%s %s:%s %s", dev.Type, maj, min, dev.Access)
			if err := clxc.setConfigItem(key, val); err != nil {
				return err
			}
		default:
			return fmt.Errorf("Invalid cgroup2 device - invalid type (allow:%t %s %s:%s %s)", dev.Allow, dev.Type, maj, min, dev.Access)
		}
	}
	return nil
}

func configureCPUController(linux *specs.LinuxCPU) error {
	// CPU resource restriction configuration
	// use strconv.FormatUint(n, 10) instead of fmt.Sprintf ?
	log.Debug().Msg("TODO configure cgroup cpu controller")
	/*
		if cpu.Shares != nil && *cpu.Shares > 0 {
				if err := clxc.setConfigItem("lxc.cgroup2.cpu.shares", fmt.Sprintf("%d", *cpu.Shares)); err != nil {
					return err
				}
		}
		if cpu.Quota != nil && *cpu.Quota > 0 {
			if err := clxc.setConfigItem("lxc.cgroup2.cpu.cfs_quota_us", fmt.Sprintf("%d", *cpu.Quota)); err != nil {
				return err
			}
		}
			if cpu.Period != nil && *cpu.Period != 0 {
				if err := clxc.setConfigItem("lxc.cgroup2.cpu.cfs_period_us", fmt.Sprintf("%d", *cpu.Period)); err != nil {
					return err
				}
			}
		if cpu.Cpus != "" {
			if err := clxc.setConfigItem("lxc.cgroup2.cpuset.cpus", cpu.Cpus); err != nil {
				return err
			}
		}
		if cpu.RealtimePeriod != nil && *cpu.RealtimePeriod > 0 {
			if err := clxc.setConfigItem("lxc.cgroup2.cpu.rt_period_us", fmt.Sprintf("%d", *cpu.RealtimePeriod)); err != nil {
				return err
			}
		}
		if cpu.RealtimeRuntime != nil && *cpu.RealtimeRuntime > 0 {
			if err := clxc.setConfigItem("lxc.cgroup2.cpu.rt_runtime_us", fmt.Sprintf("%d", *cpu.RealtimeRuntime)); err != nil {
				return err
			}
		}
	*/
	// Mems string `json:"mems,omitempty"`
	return nil
}

// https://kubernetes.io/docs/setup/production-environment/container-runtimes/
// kubelet --cgroup-driver systemd --cgroups-per-qos
type cgroupPath struct {
	Slices []string
	Scope  string
}

func (cg cgroupPath) String() string {
	return filepath.Join(append(cg.Slices, cg.Scope)...)
}

// kubernetes creates the cgroup hierarchy which can be changed by serveral cgroup related flags.
// kubepods.slice/kubepods-besteffort.slice/kubepods-besteffort-pod87f8bc68_7c18_4a1d_af9f_54eff815f688.slice
// kubepods-burstable-pod9da3b2a14682e1fb23be3c2492753207.slice:crio:fe018d944f87b227b3b7f86226962639020e99eac8991463bf7126ef8e929589
// https://github.com/cri-o/cri-o/issues/2632
func parseSystemdCgroupPath(s string) (cg cgroupPath) {
	if s == "" {
		return cg
	}
	parts := strings.Split(s, ":")

	slices := parts[0]
	for i, r := range slices {
		if r == '-' && i > 0 {
			slice := slices[0:i] + ".slice"
			cg.Slices = append(cg.Slices, slice)
		}
	}
	cg.Slices = append(cg.Slices, slices)
	if len(parts) > 0 {
		cg.Scope = strings.Join(parts[1:], "-") + ".scope"
	}
	return cg
}

func tryRemoveCgroups(c *crioLXC) {
	configItems := []string{"lxc.cgroup.dir", "lxc.cgroup.dir.container", "lxc.cgroup.dir.monitor"}
	for _, item := range configItems {
		dir := clxc.getConfigItem(item)
		if dir == "" {
			continue
		}
		err := tryRemoveAllCgroupDir(c, dir, true)
		if err != nil {
			log.Warn().Err(err).Str("lxc.config", item).Msg("failed to remove cgroup scope")
			continue
		}
		// try to remove outer directory, in case this is the POD that is deleted
		// FIXME crio should delete the kubepods slice
		outerSlice := filepath.Dir(dir)
		err = tryRemoveAllCgroupDir(c, outerSlice, false)
		if err != nil {
			log.Debug().Err(err).Str("file", outerSlice).Msg("failed to remove cgroup slice")
		}
	}
}

func tryRemoveAllCgroupDir(c *crioLXC, cgroupPath string, killProcs bool) error {
	dirName := filepath.Join("/sys/fs/cgroup", cgroupPath)
	// #nosec
	dir, err := os.Open(dirName)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if killProcs {
		err := loopKillCgroupProcs(dirName, time.Second*2)
		if err != nil {
			log.Trace().Err(err).Str("file", dirName).Msg("failed to kill cgroup procs")
		}
	}
	entries, err := dir.Readdir(-1)
	if err != nil {
		return err
	}
	// leftover lxc.pivot path
	for _, i := range entries {
		if i.IsDir() && i.Name() != "." && i.Name() != ".." {
			fullPath := filepath.Join(dirName, i.Name())
			if err := unix.Rmdir(fullPath); err != nil {
				return errors.Wrapf(err, "failed rmdir %s", fullPath)
			}
		}
	}
	return unix.Rmdir(dirName)
}

// loopKillCgroupProcs loops over PIDs in cgroup.procs and sends
// each PID the kill signal until there are no more PIDs left.
// Looping is required because processes that have been created (forked / exec)
// may not 'yet' be visible in cgroup.procs.
func loopKillCgroupProcs(scope string, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return fmt.Errorf("timeout killing processes")
		default:
			nprocs, err := killCgroupProcs(scope)
			if err != nil {
				return err
			}
			if nprocs == 0 {
				return nil
			}
			time.Sleep(time.Millisecond * 50)
		}
	}
}

// getCgroupProcs returns the PIDs for all processes which are in the
// same control group as the process for which the PID is given.
func killCgroupProcs(scope string) (int, error) {
	cgroupProcsPath := filepath.Join(scope, "cgroup.procs")
	log.Trace().Str("file", cgroupProcsPath).Msg("reading control group process list")
	// #nosec
	procsData, err := ioutil.ReadFile(cgroupProcsPath)
	if err != nil {
		return -1, errors.Wrapf(err, "failed to read control group process list %s", cgroupProcsPath)
	}
	// cgroup.procs contains one PID per line and is newline separated.
	// A trailing newline is always present.
	s := strings.TrimSpace(string(procsData))
	if s == "" {
		return 0, nil
	}
	pidStrings := strings.Split(s, "\n")
	numPids := len(pidStrings)
	if numPids == 0 {
		return 0, nil
	}

	// This indicates improper signal handling / termination of the container.
	log.Warn().Strs("pids", pidStrings).Str("cgroup", scope).Msg("killing left-over container processes")

	for _, s := range pidStrings {
		pid, err := strconv.Atoi(s)
		if err != nil {
			// Reading garbage from cgroup.procs should not happen.
			return -1, errors.Wrapf(err, "failed to convert PID %q to number", s)
		}
		if err := unix.Kill(pid, 9); err != nil {
			return -1, errors.Wrapf(err, "failed to kill %d", pid)
		}
	}

	return numPids, nil
}
