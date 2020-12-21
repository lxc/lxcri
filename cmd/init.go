package main

import (
	"fmt"
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
)

const (
	// SyncFifoPath is the path to the fifo used to block container start in init until start cmd is called.
	initDir = "/.crio-lxc"
)

func syncFifoPath() string {
	return clxc.runtimePath(initDir, "syncfifo")
}

func createFifo(dst string, uid int, gid int, mode uint32) error {
	//prevMask := unix.Umask(0000) // ?
	//defer unix.Umask(prevMask)   // ?
	if err := unix.Mkfifo(dst, mode); err != nil {
		return err
	}
	return unix.Chown(dst, uid, gid)
}

func configureInit(spec *specs.Spec) error {
	runtimeInitDir := clxc.runtimePath(initDir)
	rootfsInitDir := filepath.Join(spec.Root.Path, initDir)

	err := os.MkdirAll(rootfsInitDir, 0)
	if err != nil {
		return errors.Wrapf(err, "failed to create init dir in rootfs %q", rootfsInitDir)
	}
	// #nosec
	err = os.MkdirAll(runtimeInitDir, 0755)
	if err != nil {
		return errors.Wrapf(err, "failed to create runtime init dir %q", runtimeInitDir)
	}

	spec.Mounts = append(spec.Mounts, specs.Mount{
		Source:      runtimeInitDir,
		Destination: strings.TrimLeft(initDir, "/"),
		Type:        "bind",
		Options:     []string{"bind", "ro", "nodev", "nosuid"},
	})

	if err := clxc.setConfigItem("lxc.init.cwd", initDir); err != nil {
		return err
	}

	uid := int(spec.Process.User.UID)
	gid := int(spec.Process.User.GID)

	// create files required for crio-lxc-init
	if err := createFifo(syncFifoPath(), uid, gid, 0600); err != nil {
		return errors.Wrapf(err, "failed to create sync fifo")
	}

	if err := createList(filepath.Join(runtimeInitDir, "cmdline"), spec.Process.Args, uid, gid, 0400); err != nil {
		return err
	}
	if err := createList(filepath.Join(runtimeInitDir, "environ"), spec.Process.Env, uid, gid, 0400); err != nil {
		return err
	}
	if err := os.Symlink(spec.Process.Cwd, filepath.Join(runtimeInitDir, "cwd")); err != nil {
		return err
	}

	if err := configureInitUser(spec); err != nil {
		return err
	}

	// bind mount crio-lxc-init into the container
	initCmdPath := filepath.Join(runtimeInitDir, "init")
	err = touchFile(initCmdPath, 0)
	if err != nil {
		return errors.Wrapf(err, "failed to create %s", initCmdPath)
	}
	initCmd := filepath.Join(initDir, "init")
	spec.Mounts = append(spec.Mounts, specs.Mount{
		Source:      clxc.InitCommand,
		Destination: strings.TrimLeft(initCmd, "/"),
		Type:        "bind",
		Options:     []string{"bind", "ro", "nosuid"},
	})
	return clxc.setConfigItem("lxc.init.cmd", initCmd+" "+clxc.ContainerID)
}

func touchFile(filePath string, perm os.FileMode) error {
	// #nosec
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDONLY, perm)
	if err == nil {
		return f.Close()
	}
	return err
}

func createList(dst string, entries []string, uid int, gid int, mode uint32) error {
	// #nosec
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.Wrapf(err, "failed to create init list %s", dst)
	}

	for _, arg := range entries {
		_, err := f.WriteString(arg)
		if err != nil {
			f.Close()
			return err
		}
		_, err = f.Write([]byte{'\000'})
		if err != nil {
			f.Close()
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := unix.Chown(dst, uid, gid); err != nil {
		return err
	}
	return unix.Chmod(dst, mode)
}

func configureInitUser(spec *specs.Spec) error {
	// TODO ensure that the user namespace is enabled
	// See `man lxc.container.conf` lxc.idmap.
	for _, m := range spec.Linux.UIDMappings {
		if err := clxc.setConfigItem("lxc.idmap", fmt.Sprintf("u %d %d %d", m.ContainerID, m.HostID, m.Size)); err != nil {
			return err
		}
	}

	for _, m := range spec.Linux.GIDMappings {
		if err := clxc.setConfigItem("lxc.idmap", fmt.Sprintf("g %d %d %d", m.ContainerID, m.HostID, m.Size)); err != nil {
			return err
		}
	}

	if err := clxc.setConfigItem("lxc.init.uid", fmt.Sprintf("%d", spec.Process.User.UID)); err != nil {
		return err
	}
	if err := clxc.setConfigItem("lxc.init.gid", fmt.Sprintf("%d", spec.Process.User.GID)); err != nil {
		return err
	}

	if len(spec.Process.User.AdditionalGids) > 0 && supportsConfigItem("lxc.init.groups") {
		var b strings.Builder
		for i, gid := range spec.Process.User.AdditionalGids {
			if i > 0 {
				b.WriteByte(' ')
			}
			fmt.Fprintf(&b, "%d", gid)
		}
		if err := clxc.setConfigItem("lxc.init.groups", b.String()); err != nil {
			return err
		}
	}
	return nil
}
