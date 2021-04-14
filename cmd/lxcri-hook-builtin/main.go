package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/drachenfels-de/lxcri/pkg/specki"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

func main() {
	rootfs, _, spec, err := specki.InitHook(os.Stdin)
	if err != nil {
		panic(err)
	}

	for _, dev := range spec.Linux.Devices {
		if err := createDevice(rootfs, dev); err != nil {
			err := fmt.Errorf("failed to create device %s: %w", dev.Path, err)
			fmt.Fprintln(os.Stderr, err.Error())
		}
	}

	for _, p := range spec.Linux.MaskedPaths {
		if err := maskPath(filepath.Join(rootfs, p)); err != nil {
			err := fmt.Errorf("failed to mask path %s: %w", p, err)
			fmt.Fprintln(os.Stderr, err.Error())
		}
	}
}

func getDeviceMode(dev specs.LinuxDevice) (uint32, error) {
	var fileType uint32

	switch dev.Type {
	case "b":
		fileType = unix.S_IFBLK
	case "c":
		fileType = unix.S_IFCHR
	case "p":
		fileType = unix.S_IFIFO
	default:
		return 0, fmt.Errorf("unsupported device type: %s", dev.Type)
	}

	var perm uint32 = 0666
	if dev.FileMode == nil {
		perm = uint32(*dev.FileMode)
	}
	return (fileType | perm), nil
}

func createDevice(rootfs string, dev specs.LinuxDevice) error {
	mode, err := getDeviceMode(dev)
	if err != nil {
		return err
	}

	// ignored by unix.Mknod if dev.Type is not unix.S_IFBLK or unix.S_IFCHR
	mkdev := int(unix.Mkdev(uint32(dev.Major), uint32(dev.Minor)))

	err = unix.Mknod(filepath.Join(rootfs, dev.Path), mode, mkdev)
	if err != nil {
		return fmt.Errorf("mknod failed: %s", err)
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
