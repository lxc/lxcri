package lxcontainer

import (
	"fmt"
	"os"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func isDeviceEnabled(c *Container, dev specs.LinuxDevice) bool {
	for _, specDev := range c.Linux.Devices {
		if specDev.Path == dev.Path {
			return true
		}
	}
	return false
}

func addDevice(c *Container, dev specs.LinuxDevice, mode os.FileMode, uid uint32, gid uint32, access string) {
	dev.FileMode = &mode
	dev.UID = &uid
	dev.GID = &gid
	c.Linux.Devices = append(c.Linux.Devices, dev)

	addDevicePerms(c, dev.Type, &dev.Major, &dev.Minor, access)
}

func addDevicePerms(c *Container, devType string, major *int64, minor *int64, access string) {
	devCgroup := specs.LinuxDeviceCgroup{Allow: true, Type: devType, Major: major, Minor: minor, Access: access}
	c.Linux.Resources.Devices = append(c.Linux.Resources.Devices, devCgroup)
}

// ensureDefaultDevices adds the mandatory devices defined by the [runtime spec](https://github.com/opencontainers/runtime-spec/blob/master/config-linux.md#default-devices)
// to the given container spec if required.
// crio can add devices to containers, but this does not work for privileged containers.
// See https://github.com/cri-o/cri-o/blob/a705db4c6d04d7c14a4d59170a0ebb4b30850675/server/container_create_linux.go#L45
// TODO file an issue on cri-o (at least for support)
func ensureDefaultDevices(c *Container) error {
	mode := os.FileMode(0666)
	var uid, gid uint32 = c.Process.User.UID, c.Process.User.GID

	devices := []specs.LinuxDevice{
		specs.LinuxDevice{Path: "/dev/null", Type: "c", Major: 1, Minor: 3},
		specs.LinuxDevice{Path: "/dev/zero", Type: "c", Major: 1, Minor: 5},
		specs.LinuxDevice{Path: "/dev/full", Type: "c", Major: 1, Minor: 7},
		specs.LinuxDevice{Path: "/dev/random", Type: "c", Major: 1, Minor: 8},
		specs.LinuxDevice{Path: "/dev/urandom", Type: "c", Major: 1, Minor: 9},
		specs.LinuxDevice{Path: "/dev/tty", Type: "c", Major: 5, Minor: 0},
		// FIXME runtime mandates that /dev/ptmx should be bind mount from host - why ?
		// `man 2 mount` | devpts
		// ` To use this option effectively, /dev/ptmx must be a symbolic link to pts/ptmx.
		// See Documentation/filesystems/devpts.txt in the Linux kernel source tree for details.`
	}

	ptmx := specs.LinuxDevice{Path: "/dev/ptmx", Type: "c", Major: 5, Minor: 2}
	addDevicePerms(c, "c", &ptmx.Major, &ptmx.Minor, "rwm") // /dev/ptmx, /dev/pts/ptmx

	pts0 := specs.LinuxDevice{Path: "/dev/pts/0", Type: "c", Major: 88, Minor: 0}
	addDevicePerms(c, "c", &pts0.Major, nil, "rwm") // dev/pts/[0..9]

	if c.Linux.Resources == nil {
		c.Linux.Resources = &specs.LinuxResources{}
	}

	// add missing default devices
	for _, dev := range devices {
		if !isDeviceEnabled(c, dev) {
			addDevice(c, dev, mode, uid, gid, "rwm")
		}
	}
	return nil
}

func writeDevices(dst string, c *Container) error {
	if c.Linux.Devices == nil {
		return nil
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	for _, d := range c.Linux.Devices {
		uid := c.Process.User.UID
		if d.UID != nil {
			uid = *d.UID
		}
		gid := c.Process.User.GID
		if d.GID != nil {
			gid = *d.GID
		}
		mode := os.FileMode(0600)
		if d.FileMode != nil {
			mode = *d.FileMode
		}
		_, err = fmt.Fprintf(f, "%s %s %d %d %o %d:%d\n", d.Path, d.Type, d.Major, d.Minor, mode, uid, gid)
		if err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}
