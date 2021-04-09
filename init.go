package lxcri

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

const (
	// initDir is the working directory for lxcri-init.
	// It contains the init binary itself and all files required for it.
	initDir = "/.lxcri"
)

func createFifo(dst string, mode uint32) error {
	if err := unix.Mkfifo(dst, mode); err != nil {
		return errorf("mkfifo dst:%s failed: %w", dst, err)
	}
	// lxcri-init must be able to write to the fifo.
	// Init process UID/GID can be different from runtime process UID/GID
	// liblxc changes the owner of the runtime directory to the effective container UID.
	// access to the files is protected by the runtimeDir
	// because umask (0022) affects unix.Mkfifo, a separate chmod is required
	// FIXME if container UID equals os.GetUID() and spec.
	if err := unix.Chmod(dst, mode); err != nil {
		return errorf("chmod mkfifo failed: %w", err)
	}
	return nil
}

func configureInit(rt *Runtime, c *Container) error {
	runtimeInitDir := c.RuntimePath(initDir)
	//rootfsInitDir := filepath.Join(c.Root.Path, initDir)

	/*
		err := os.MkdirAll(rootfsInitDir, 0755)
		if err != nil {
			return fmt.Errorf("failed to create init dir in rootfs %q: %w", rootfsInitDir, err)
		}
	*/
	// #nosec
	err := os.MkdirAll(runtimeInitDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create runtime init dir %q: %w", runtimeInitDir, err)
	}

	c.Mounts = append(c.Mounts, specs.Mount{
		Source:      runtimeInitDir,
		Destination: strings.TrimLeft(initDir, "/"),
		Type:        "bind",
		Options:     []string{"bind", "ro", "nodev", "nosuid", "create=dir"},
	})

	if err := c.SetConfigItem("lxc.init.cwd", initDir); err != nil {
		return err
	}

	// create files required for lxcri-init
	if rt.runAsRuntimeUser(c) {
		if err := createFifo(c.syncFifoPath(), 0600); err != nil {
			return fmt.Errorf("failed to create sync fifo: %w", err)
		}
		if err := createList(filepath.Join(runtimeInitDir, "cmdline"), c.Process.Args, 0400); err != nil {
			return err
		}
		if err := createList(filepath.Join(runtimeInitDir, "environ"), c.Process.Env, 0400); err != nil {
			return err
		}
	} else {
		if err := createFifo(c.syncFifoPath(), 0666); err != nil {
			return fmt.Errorf("failed to create sync fifo: %w", err)
		}
		if err := createList(filepath.Join(runtimeInitDir, "cmdline"), c.Process.Args, 0444); err != nil {
			return err
		}
		if err := createList(filepath.Join(runtimeInitDir, "environ"), c.Process.Env, 0444); err != nil {
			return err
		}
	}

	if err := os.Symlink(c.Process.Cwd, filepath.Join(runtimeInitDir, "cwd")); err != nil {
		return err
	}

	if c.Annotations != nil {
		msgPath := c.Annotations["io.kubernetes.container.terminationMessagePath"]
		if msgPath != "" {
			if err := os.Symlink(msgPath, filepath.Join(runtimeInitDir, "error.log")); err != nil {
				return err
			}
		}
	}

	if err := configureInitUser(c); err != nil {
		return err
	}

	// bind mount lxcri-init into the container
	initCmdPath := filepath.Join(runtimeInitDir, "init")
	err = touchFile(initCmdPath, 0)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", initCmdPath, err)
	}
	initCmd := filepath.Join(initDir, "init")
	c.Mounts = append(c.Mounts, specs.Mount{
		Source:      rt.libexec(ExecInit),
		Destination: strings.TrimLeft(initCmd, "/"),
		Type:        "bind",
		Options:     []string{"bind", "ro", "nosuid"},
	})
	return c.SetConfigItem("lxc.init.cmd", initCmd+" "+c.ContainerID)
}

func touchFile(filePath string, perm os.FileMode) error {
	// #nosec
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDONLY, perm)
	if err == nil {
		return f.Close()
	}
	return err
}

func createList(dst string, entries []string, mode uint32) error {
	// #nosec
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errorf("failed to create %s: %w", dst, err)
	}

	for _, arg := range entries {
		_, err := f.WriteString(arg)
		if err != nil {
			f.Close()
			return errorf("failed to write to %s: %w", dst, err)
		}
		_, err = f.Write([]byte{'\000'})
		if err != nil {
			f.Close()
			return errorf("failed to write to %s: %w", dst, err)
		}
	}
	if err := f.Close(); err != nil {
		return errorf("failed to close %s: %w", dst, err)
	}
	if err := unix.Chmod(dst, mode); err != nil {
		return errorf("failed to chmod %s mode:%o : %w", dst, mode, err)
	}
	return nil
}

func configureInitUser(c *Container) error {
	// TODO ensure that the user namespace is enabled
	// See `man lxc.container.conf` lxc.idmap.
	for _, m := range c.Linux.UIDMappings {
		if err := c.SetConfigItem("lxc.idmap", fmt.Sprintf("u %d %d %d", m.ContainerID, m.HostID, m.Size)); err != nil {
			return err
		}
	}

	for _, m := range c.Linux.GIDMappings {
		if err := c.SetConfigItem("lxc.idmap", fmt.Sprintf("g %d %d %d", m.ContainerID, m.HostID, m.Size)); err != nil {
			return err
		}
	}

	if err := c.SetConfigItem("lxc.init.uid", fmt.Sprintf("%d", c.Process.User.UID)); err != nil {
		return err
	}
	if err := c.SetConfigItem("lxc.init.gid", fmt.Sprintf("%d", c.Process.User.GID)); err != nil {
		return err
	}

	if len(c.Process.User.AdditionalGids) > 0 && c.SupportsConfigItem("lxc.init.groups") {
		var b strings.Builder
		for i, gid := range c.Process.User.AdditionalGids {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%d", gid)
		}
		if err := c.SetConfigItem("lxc.init.groups", b.String()); err != nil {
			return err
		}
	}
	return nil
}
