package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
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
		log.Warn().Msg("TODO cgroup blockio controller not implemented")
	}

	if hugetlb := spec.Linux.Resources.HugepageLimits; hugetlb != nil {
		// set Hugetlb limit (in bytes)
		log.Warn().Msg("TODO cgroup hugetlb controller not implemented")
	}
	if net := spec.Linux.Resources.Network; net != nil {
		log.Warn().Msg("TODO cgroup network controller not implemented")
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
