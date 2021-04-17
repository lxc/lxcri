package lxcri

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	//"github.com/fsnotify/fsnotify"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

var cgroupRoot string

func detectCgroupRoot() string {
	if err := isFilesystem("/sys/fs/cgroup", "cgroup2"); err == nil {
		return "/sys/fs/cgroup"
	}
	if err := isFilesystem("/sys/fs/cgroup/unified", "cgroup2"); err == nil {
		return "/sys/fs/cgroup/unified"
	}
	return ""
}

func init() {
	cgroupRoot = detectCgroupRoot()
}

// https://github.com/opencontainers/runtime-spec/blob/v1.0.2/config-linux.md
// TODO New spec will contain a property Unified for cgroupv2 properties
// https://github.com/opencontainers/runtime-spec/blob/master/config-linux.md#unified
func configureCgroup(rt *Runtime, c *Container) error {
	if err := configureCgroupPath(rt, c); err != nil {
		return err
	}

	if devices := c.Spec.Linux.Resources.Devices; devices != nil {
		if rt.Features.CgroupDevices {
			if err := configureDeviceController(c); err != nil {
				return err
			}
		} else {
			c.Log.Warn().Msg("cgroup device controller feature is disabled - access to all devices is granted")
		}

	}

	if mem := c.Spec.Linux.Resources.Memory; mem != nil {
		c.Log.Debug().Msg("TODO cgroup memory controller not implemented")
	}

	if cpu := c.Spec.Linux.Resources.CPU; cpu != nil {
		if err := configureCPUController(rt, cpu); err != nil {
			return err
		}
	}

	if pids := c.Spec.Linux.Resources.Pids; pids != nil {
		if err := c.SetConfigItem("lxc.cgroup2.pids.max", fmt.Sprintf("%d", pids.Limit)); err != nil {
			return err
		}
	}
	if blockio := c.Spec.Linux.Resources.BlockIO; blockio != nil {
		c.Log.Debug().Msg("TODO cgroup blockio controller not implemented")
	}

	if hugetlb := c.Spec.Linux.Resources.HugepageLimits; hugetlb != nil {
		// set Hugetlb limit (in bytes)
		c.Log.Debug().Msg("TODO cgroup hugetlb controller not implemented")
	}
	if net := c.Spec.Linux.Resources.Network; net != nil {
		c.Log.Debug().Msg("TODO cgroup network controller not implemented")
	}
	return nil
}

func configureCgroupPath(rt *Runtime, c *Container) error {
	if rt.SystemdCgroup {
		c.CgroupDir = parseSystemdCgroupPath(c.Spec.Linux.CgroupsPath)
	} else {
		c.CgroupDir = c.Spec.Linux.CgroupsPath
	}

	if err := c.SetConfigItem("lxc.cgroup.relative", "0"); err != nil {
		return err
	}

	// @since lxc @a900cbaf257c6a7ee9aa73b09c6d3397581d38fb
	// checking for on of the config items shuld be enough, because they were introduced together ...
	//  lxc.cgroup.dir.payload and lxc.cgroup.dir.monitor
	splitCgroup := c.SupportsConfigItem("lxc.cgroup.dir.container", "lxc.cgroup.dir.monitor")

	if !splitCgroup || rt.MonitorCgroup == "" {
		return c.SetConfigItem("lxc.cgroup.dir", c.CgroupDir)
	}

	c.MonitorCgroupDir = filepath.Join(rt.MonitorCgroup, c.ContainerID+".scope")

	if err := c.SetConfigItem("lxc.cgroup.dir.container", c.CgroupDir); err != nil {
		return err
	}
	if err := c.SetConfigItem("lxc.cgroup.dir.monitor", c.MonitorCgroupDir); err != nil {
		return err
	}

	if c.SupportsConfigItem("lxc.cgroup.dir.monitor.pivot") {
		if err := c.SetConfigItem("lxc.cgroup.dir.monitor.pivot", rt.MonitorCgroup); err != nil {
			return err
		}
	}
	return nil

}

func configureDeviceController(c *Container) error {
	devicesAllow := "lxc.cgroup2.devices.allow"
	devicesDeny := "lxc.cgroup2.devices.deny"

	// Set cgroup device permissions from spec.
	// Device rule parsing in LXC is not well documented in lxc.container.conf
	// see https://github.com/lxc/lxc/blob/79c66a2af36ee8e967c5260428f8cdb5c82efa94/src/lxc/cgroups/cgfsng.c#L2545
	// Mixing allow/deny is not permitted by lxc.cgroup2.devices.
	// Best practise is to build up an allow list to disable access restrict access to new/unhandled devices.

	anyDevice := ""
	blockDevice := "b"
	charDevice := "c"

	for _, dev := range c.Spec.Linux.Resources.Devices {
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
			if err := c.SetConfigItem(key, val); err != nil {
				return err
			}
			val = fmt.Sprintf("%s %s:%s %s", charDevice, maj, min, dev.Access)
			if err := c.SetConfigItem(key, val); err != nil {
				return err
			}
		case blockDevice, charDevice:
			val := fmt.Sprintf("%s %s:%s %s", dev.Type, maj, min, dev.Access)
			if err := c.SetConfigItem(key, val); err != nil {
				return err
			}
		default:
			return fmt.Errorf("invalid cgroup2 device - invalid type (allow:%t %s %s:%s %s)", dev.Allow, dev.Type, maj, min, dev.Access)
		}
	}
	return nil
}

func configureCPUController(clxc *Runtime, slinux *specs.LinuxCPU) error {
	// CPU resource restriction configuration
	// use strconv.FormatUint(n, 10) instead of fmt.Sprintf ?
	clxc.Log.Debug().Msg("TODO configure cgroup cpu controller")
	/*
		if cpu.Shares != nil && *cpu.Shares > 0 {
				if err := clxc.SetConfigItem("lxc.cgroup2.cpu.shares", fmt.Sprintf("%d", *cpu.Shares)); err != nil {
					return err
				}
		}
		if cpu.Quota != nil && *cpu.Quota > 0 {
			if err := clxc.SetConfigItem("lxc.cgroup2.cpu.cfs_quota_us", fmt.Sprintf("%d", *cpu.Quota)); err != nil {
				return err
			}
		}
			if cpu.Period != nil && *cpu.Period != 0 {
				if err := clxc.SetConfigItem("lxc.cgroup2.cpu.cfs_period_us", fmt.Sprintf("%d", *cpu.Period)); err != nil {
					return err
				}
			}
		if cpu.Cpus != "" {
			if err := clxc.SetConfigItem("lxc.cgroup2.cpuset.cpus", cpu.Cpus); err != nil {
				return err
			}
		}
		if cpu.RealtimePeriod != nil && *cpu.RealtimePeriod > 0 {
			if err := clxc.SetConfigItem("lxc.cgroup2.cpu.rt_period_us", fmt.Sprintf("%d", *cpu.RealtimePeriod)); err != nil {
				return err
			}
		}
		if cpu.RealtimeRuntime != nil && *cpu.RealtimeRuntime > 0 {
			if err := clxc.SetConfigItem("lxc.cgroup2.cpu.rt_runtime_us", fmt.Sprintf("%d", *cpu.RealtimeRuntime)); err != nil {
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
// TODO Where is the systemd cgroup path encoding officially documented?
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

// killCgroup freezes the cgroups of the given container
// and sends the given signal sig to all cgroup members.
func killCgroup(ctx context.Context, c *Container, sig unix.Signal) error {
	if c.CgroupDir == "" {
		return nil
	}
	rootDir := filepath.Join(cgroupRoot, c.CgroupDir)
	eventsFile := filepath.Join(rootDir, "cgroup.events")

	ev, err := parseCgroupEvents(eventsFile)
	if err != nil {
		return err
	}
	if !ev.populated {
		return nil
	}

	freezer := filepath.Join(rootDir, "cgroup.freeze")

	err = cgroupFreeze(freezer, true)
	if err != nil {
		return err
	}

	err = pollCgroupEvents(ctx, eventsFile, func(ev cgroupEvents) bool {
		return ev.frozen
	})
	if err != nil {
		return err
	}

	err = filepath.Walk(rootDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() != "cgroup.procs" {
			return nil
		}
		procsData, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// cgroup.procs contains one PID per line and is newline separated.
		// A trailing newline is always present.
		s := strings.TrimSpace(string(procsData))
		if s == "" {
			return nil
		}
		vals := strings.Split(s, "\n")

		c.Log.Debug().Msgf("killing %d cgroup procs: %s", len(vals), vals)
		for _, s := range vals {
			pid, err := strconv.Atoi(s)
			if err != nil {
				c.Log.Error().Msgf("failed to convert PID %q to number: %s", s, err)
				continue
			}
			// do not kill the monitor process
			if pid == c.Pid {
				continue
			}
			err = unix.Kill(pid, sig)
			if err != nil && err != unix.ESRCH {
				c.Log.Error().Msgf("failed to kill %d: %s", pid, err)
				continue
			}
		}
		return nil
	})

	if err != nil {
		return err
	}

	err = cgroupFreeze(freezer, false)
	if err != nil {
		return err
	}

	return nil
}

type cgroupEvents struct {
	frozen    bool
	populated bool
}

func parseCgroupEvents(filename string) (cgroupEvents, error) {
	ev := cgroupEvents{}
	data, err := os.ReadFile(filename)
	if err != nil {
		return ev, err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		switch line {
		case "populated 0":
			ev.populated = false
		case "populated 1":
			ev.populated = true
		case "frozen 0":
			ev.frozen = false
		case "frozen 1":
			ev.frozen = true
		}
	}
	return ev, nil
}

func cgroupFreeze(filename string, freeze bool) error {
	f, err := os.OpenFile(filename, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if freeze {
		_, err = f.Write([]byte("1"))
	} else {
		_, err = f.Write([]byte("0"))
	}
	return err
}

func pollCgroupEvents(ctx context.Context, eventsFile string, fn func(ev cgroupEvents) bool) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			ev, err := parseCgroupEvents(eventsFile)
			if err != nil {
				return err
			}
			if fn(ev) {
				return nil
			}
			time.Sleep(time.Millisecond * 5)
		}
	}
}

func deleteCgroup(cgroupName string) error {
	return deleteCgroupRecursive(cgroupName, 0, 10)
}

func deleteCgroupRecursive(cgroupName string, level, max int) error {
	if level == max {
		return fmt.Errorf("reached max recursion of %d", max)
	}
	dirName := filepath.Join(cgroupRoot, cgroupName)
	dir, err := os.Open(dirName)
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
		if !i.IsDir() {
			continue
		}
		name := i.Name()
		if name == "." || name == ".." {
			continue
		}
		childGroup := filepath.Join(cgroupName, name)
		err := deleteCgroupRecursive(childGroup, level+1, max)
		if err != nil {
			return err
		}
	}
	return unix.Rmdir(dirName)
}
