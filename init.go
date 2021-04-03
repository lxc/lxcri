package lxcri

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/opencontainers/runtime-spec/specs-go"
)

const (
	// initDir is the working directory for lxcri-init.
	// It contains the init binary itself and all files required for it.
	initDir = "/.lxcri"
)

func createFifo(dst string, uid int, gid int, mode uint32) error {
	if err := unix.Mkfifo(dst, mode); err != nil {
		return fmt.Errorf("mkfifo dst:%s mode:%o failed: %w", dst, mode, err)
	}
	if err := unix.Chown(dst, uid, gid); err != nil {
		return fmt.Errorf("chown uid:%d gid:%d dst:%s failed: %w", uid, gid, dst, err)
	}
	return nil
}

func configureInit(rt *Runtime, c *Container) error {
	runtimeInitDir := c.RuntimePath(initDir)
	rootfsInitDir := filepath.Join(c.Root.Path, initDir)

	err := os.MkdirAll(rootfsInitDir, 0)
	if err != nil {
		return fmt.Errorf("failed to create init dir in rootfs %q: %w", rootfsInitDir, err)
	}
	// #nosec
	err = os.MkdirAll(runtimeInitDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create runtime init dir %q: %w", runtimeInitDir, err)
	}

	c.Mounts = append(c.Mounts, specs.Mount{
		Source:      runtimeInitDir,
		Destination: strings.TrimLeft(initDir, "/"),
		Type:        "bind",
		Options:     []string{"bind", "ro", "nodev", "nosuid"},
	})

	if err := c.SetConfigItem("lxc.init.cwd", initDir); err != nil {
		return err
	}

	uid := int(c.Process.User.UID)
	gid := int(c.Process.User.GID)

	// create files required for lxcri-init
	if err := createFifo(c.syncFifoPath(), uid, gid, 0600); err != nil {
		return fmt.Errorf("failed to create sync fifo: %w", err)
	}

	if err := createList(filepath.Join(runtimeInitDir, "cmdline"), c.Process.Args, uid, gid, 0400); err != nil {
		return err
	}
	if err := createList(filepath.Join(runtimeInitDir, "environ"), c.Process.Env, uid, gid, 0400); err != nil {
		return err
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

func createList(dst string, entries []string, uid int, gid int, mode uint32) error {
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
	if err := unix.Chown(dst, uid, gid); err != nil {
		return errorf("failed to chown %s uid:%d gid:%d :%w", dst, uid, gid, err)
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
