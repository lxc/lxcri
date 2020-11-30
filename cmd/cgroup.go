package main

import (
	"bytes"
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
// kubernetes creates the cgroup hierarchy which can be changed by serveral cgroup related flags.
// kubepods.slice/kubepods-besteffort.slice/kubepods-besteffort-pod87f8bc68_7c18_4a1d_af9f_54eff815f688.slice
// kubepods-burstable-pod9da3b2a14682e1fb23be3c2492753207.slice:crio:fe018d944f87b227b3b7f86226962639020e99eac8991463bf7126ef8e929589
// https://github.com/cri-o/cri-o/issues/2632
func parseSystemdCgroupPath(s string) string {
	parts := strings.Split(s, ":")

	var cgPath []string

	for i, r := range parts[0] {
		if r == '-' && i > 0 {
			cgPath = append(cgPath, parts[0][0:i]+".slice")
		}
	}
	cgPath = append(cgPath, parts[0])
	if len(parts) > 1 {
		cgPath = append(cgPath, strings.Join(parts[1:], "-")+".scope")
	}
	return filepath.Join(cgPath...)
}

type cgroupInfo struct {
	Name  string
	Procs []int
	// controllers
}

func (cg *cgroupInfo) loadProcs() error {
	cgroupProcsPath := filepath.Join("/sys/fs/cgroup", cg.Name, "cgroup.procs")
	// #nosec
	procsData, err := ioutil.ReadFile(cgroupProcsPath)
	if err != nil {
		return errors.Wrapf(err, "failed to read control group process list %s", cgroupProcsPath)
	}
	// cgroup.procs contains one PID per line and is newline separated.
	// A trailing newline is always present.
	s := strings.TrimSpace(string(procsData))
	if s == "" {
		return nil
	}
	pidStrings := strings.Split(s, "\n")
	cg.Procs = make([]int, 0, len(pidStrings))
	for _, s := range pidStrings {
		pid, err := strconv.Atoi(s)
		if err != nil {
			return errors.Wrapf(err, "failed to convert PID %q to number", s)
		}
		cg.Procs = append(cg.Procs, pid)
	}
	return nil
}

func loadCgroup(cgName string) (*cgroupInfo, error) {
	info := &cgroupInfo{Name: cgName}
	if err := info.loadProcs(); err != nil {
		return nil, err
	}
	return info, nil
}

func killCgroupProcs(cgroupName string, sig unix.Signal) error {
	dirName := filepath.Join("/sys/fs/cgroup", cgroupName)
	// #nosec
	dir, err := os.Open(dirName)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	entries, err := dir.Readdir(-1)
	if err := dir.Close(); err != nil {
		return err
	}
	if err != nil {
		return err
	}
	for _, i := range entries {
		if i.IsDir() && i.Name() != "." && i.Name() != ".." {
			fullPath := filepath.Join(dirName, i.Name())
			cg, err := loadCgroup(filepath.Join(cgroupName, i.Name()))
			if err != nil {
				log.Warn().Err(err).Str("file", fullPath).Msg("failed to read cgroup proces")
				return err
			}
			for _, pid := range cg.Procs {
				log.Warn().Int("pid", pid).Msg("killing left-over process")
				err := unix.Kill(pid, sig)
				if err != nil && err != unix.ESRCH {
					return errors.Wrapf(err, "failed to kill %d", pid)
				}
			}
		}
	}
	return nil
}

// TODO maybe use polling instead
// fds := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLIN}}
// n, err := unix.Poll(fds, timeout)
func drainCgroup(cgroupName string, sig unix.Signal, timeout time.Duration) error {
	p := filepath.Join("/sys/fs/cgroup", cgroupName, "cgroup.events")
	f, err := os.OpenFile(p, os.O_RDONLY, 0)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	buf.Grow(64)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		buf.Reset()
		_, err = f.Seek(0, os.SEEK_SET)
		if err != nil {
			return err
		}
		_, err := buf.ReadFrom(f)
		if err != nil {
			return err
		}

		for _, line := range strings.Split(buf.String(), "\n") {
			if line == "populated 0" {
				return nil
			}
		}
		err = killCgroupProcs(cgroupName, sig)
		if err != nil {
			return errors.Wrapf(err, "failed to kill cgroup procs %s", cgroupName)
		}
		log.Trace().Str("cgroup", cgroupName).Msg("waiting for cgroup to drain")
		time.Sleep(time.Millisecond * 50)
	}
	return fmt.Errorf("timeout")
}

func deleteCgroup(cgroupName string) error {
	dirName := filepath.Join("/sys/fs/cgroup", cgroupName)
	// #nosec
	dir, err := os.Open(dirName)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	entries, err := dir.Readdir(-1)
	if err := dir.Close(); err != nil {
		return err
	}
	if err != nil {
		return err
	}
	for _, i := range entries {
		if i.IsDir() && i.Name() != "." && i.Name() != ".." {
			p := filepath.Join(dirName, i.Name())
			err := unix.Rmdir(p)
			if err != nil && !os.IsNotExist(err) {
				log.Warn().Err(err).Str("file", p).Msg("failed to remove cgroup dir")
				return err
			}
		}
	}
	return unix.Rmdir(dirName)
}
