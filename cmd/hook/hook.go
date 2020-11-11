package main

import (
	"fmt"
	"github.com/lxc/crio-lxc/cmd/internal"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

func fail(err error, details string) {
	msg := fmt.Errorf("ERR: %s failed: %s", details, err.Error())
	panic(msg)
}

func main() {
	// get rootfs mountpoint from environment
	rootfs := os.Getenv("LXC_ROOTFS_MOUNT")
	if rootfs == "" {
		panic("LXC_ROOTFS_MOUNT environment is not set")
	}

	if _, err := os.Stat(rootfs); err != nil {
		fail(err, "stat for rootfs mount failed "+rootfs)
	}

	specPath := filepath.Join(rootfs, internal.INIT_SPEC)
	spec, err := internal.ReadSpec(specPath)
	if err != nil {
		fail(err, "parse spec "+specPath)
	}

	for _, dev := range spec.Linux.Devices {
		dev.Path = filepath.Join(rootfs, dev.Path)
		if err := createDevice(spec, dev); err != nil {
			fail(err, "failed to create device "+dev.Path)
		}
	}

	for _, p := range spec.Linux.MaskedPaths {
		rp := filepath.Join(rootfs, p)
		if err := maskPath(rp); err != nil {
			fail(err, "failed to mask path "+rp)
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
