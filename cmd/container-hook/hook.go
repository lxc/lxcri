package main

import (
	"fmt"
	"github.com/lxc/crio-lxc/cmd/internal"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

// from `man lxc.container.conf`
// Standard output from the hooks is logged at debug level.
// Standard error is not logged ...
func fail(err error, msg string, args ...interface{}) {
	if err == nil {
		fmt.Printf("ERR "+msg+"\n", args...)
	} else {
		fmt.Printf("ERR:"+msg+": %s\n", append(args, err))
	}
	os.Exit(1)
}

func main() {
	// get rootfs mountpoint from environment
	rootfs := os.Getenv("LXC_ROOTFS_MOUNT")
	if rootfs == "" {
		fail(nil, "LXC_ROOTFS_MOUNT environment is not set")
	}

	// ensure we are running in the correct hook
	if hook := os.Getenv("LXC_HOOK_TYPE"); hook != "mount" {
		fail(nil, "LXC_HOOK_TYPE=%s but can only run in 'mount' hook", hook)
	}

	if _, err := os.Stat(rootfs); err != nil {
		fail(err, "stat for rootfs mount %q failed", rootfs)
	}

	specPath := filepath.Join(rootfs, internal.InitSpec)
	spec, err := internal.ReadSpec(specPath)
	if err != nil {
		fail(err, "failed to parse spec %s", specPath)
	}

	for _, dev := range spec.Linux.Devices {
		dev.Path = filepath.Join(rootfs, dev.Path)
		if err := createDevice(spec, dev); err != nil {
			fail(err, "failed to create device %s", dev.Path)
		}
	}

	for _, p := range spec.Linux.MaskedPaths {
		rp := filepath.Join(rootfs, p)
		if err := maskPath(rp); err != nil {
			fail(err, "failed to mask path %s", rp)
		}
	}
}

func getDeviceType(s string) int {
	switch s {
	case "b":
		return unix.S_IFBLK
	case "c":
		return unix.S_IFCHR
	case "p":
		return unix.S_IFIFO
		// case "u": ? unbuffered character device ?
	}
	return -1
}

func createDevice(spec *specs.Spec, dev specs.LinuxDevice) error {
	var mode uint32 = 0660
	if dev.FileMode != nil {
		mode |= uint32(*dev.FileMode)
	}
	devType := getDeviceType(dev.Type)
	if devType == -1 {
		return fmt.Errorf("unsupported device type: %s", dev.Type)
	}
	mode |= uint32(devType)

	devMode := 0
	if devType == unix.S_IFBLK || devType == unix.S_IFCHR {
		devMode = int(unix.Mkdev(uint32(dev.Major), uint32(dev.Minor)))
	}

	// ignore error (mknod will fail)
	// #nosec
	os.MkdirAll(filepath.Dir(dev.Path), 0755)

	err := unix.Mknod(dev.Path, mode, devMode)
	if err != nil {
		return fmt.Errorf("mknod failed: %s", err)
	}

	uid := spec.Process.User.UID
	if dev.UID != nil {
		uid = *dev.UID
	}
	gid := spec.Process.User.GID
	if dev.GID != nil {
		gid = *dev.GID
	}
	err = unix.Chown(dev.Path, int(uid), int(gid))
	if err != nil {
		return fmt.Errorf("chown failed: %s", err)
	}
	return nil
}

func maskPath(p string) error {
	err := unix.Mount("/dev/null", p, "", unix.MS_BIND, "")
	if os.IsNotExist(err) {
		return nil
	}
	if err == unix.ENOTDIR {
		return unix.Mount("tmpfs", p, "tmpfs", unix.MS_RDONLY, "")
	}
	return err
}
