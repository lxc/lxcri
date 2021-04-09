package lxcri

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func removeMountOptions(rt *Runtime, fs string, opts []string, unsupported ...string) []string {
	supported := make([]string, 0, len(opts))
	for _, opt := range opts {
		addOption := true
		for _, u := range unsupported {
			if opt == u {
				addOption = false
				break
			}
		}
		if addOption {
			supported = append(supported, opt)
		} else {
			rt.Log.Info().Str("fs", fs).Str("option", opt).Msg("removed mount option")
		}
	}
	return supported
}

func filterMountOptions(rt *Runtime, fs string, opts []string) []string {
	switch fs {
	case "sysfs":
		return removeMountOptions(rt, fs, opts, "rslave")
	case "tmpfs":
		// TODO make this configurable per filesystem
		return removeMountOptions(rt, fs, opts, "rprivate", "tmpcopyup")
	case "cgroup2":
		// TODO make this configurable per filesystem
		return removeMountOptions(rt, fs, opts, "private", "rslave")
	}
	return opts
}

func configureMounts(rt *Runtime, c *Container) error {
	// excplicitly disable auto-mounting
	if err := c.SetConfigItem("lxc.mount.auto", ""); err != nil {
		return err
	}

	for i := range c.Spec.Mounts {
		ms := c.Spec.Mounts[i]
		if ms.Type == "cgroup" {
			// TODO check if hieararchy is cgroup v2 only (unified mode)
			ms.Type = "cgroup2"
			ms.Source = "cgroup2"
			// cgroup filesystem is automounted even with lxc.rootfs.managed = 0
			// from 'man lxc.container.conf':
			// If cgroup namespaces are enabled, then any cgroup auto-mounting request will be ignored,
			// since the container can mount the filesystems itself, and automounting can confuse the container.
		}

		// TODO replace with symlink.FollowSymlinkInScope(filepath.Join(rootfs, "/etc/passwd"), rootfs) ?
		// "github.com/docker/docker/pkg/symlink"
		mountDest, err := resolveMountDestination(c.Spec.Root.Path, ms.Destination)
		// Intermediate path resolution failed. This is not an error, since
		// the remaining directories / files are automatically created (create=dir|file)
		rt.Log.Trace().Err(err).Str("file", ms.Destination).Str("target", mountDest).Msg("resolve mount destination")

		// Check whether the resolved destination of the target link escapes the rootfs.
		if !filepath.HasPrefix(mountDest, c.Spec.Root.Path) {
			// refuses mount destinations that escape from rootfs
			return fmt.Errorf("resolved mount target path %s escapes from container root %s", mountDest, c.Spec.Root.Path)
		}

		ms.Destination = mountDest

		if err := createMountDestination(c, &ms); err != nil {
			return err
		}

		ms.Options = filterMountOptions(rt, ms.Type, ms.Options)

		mnt := fmt.Sprintf("%s %s %s %s", ms.Source, ms.Destination, ms.Type, strings.Join(ms.Options, ","))

		if err := c.SetConfigItem("lxc.mount.entry", mnt); err != nil {
			return err
		}
	}
	return nil
}

// createMountDestination creates non-existent mount destination paths.
// This is required if rootfs is mounted readonly.
// When the source is a file that should be bind mounted a destination file is created.
// In any other case a target directory is created.
// We add 'create=dir' or 'create=file' to mount options because the mount destination
// may be shadowed by a previous mount. In this case lxc will create the mount destination.
// TODO check whether this is  desired behaviour in lxc ?
// Shouldn't the rootfs should be mounted readonly after all mounts destination directories have been created ?
// https://github.com/lxc/lxc/issues/1702
func createMountDestination(c *Container, ms *specs.Mount) error {
	info, err := os.Stat(ms.Source)

	// source for bind mount must exist
	if err != nil && ms.Type == "bind" {
		for _, o := range ms.Options {
			if o == "optional" {
				return nil
			}
		}
		return errorf("failed to access bind mount source %s: %w", ms.Source, err)
	}

	if err != nil || info.IsDir() {
		ms.Options = append(ms.Options, "create=dir")
		if c.Spec.Root.Readonly {
			return os.MkdirAll(ms.Destination, 0755)
		}
		return nil
	}

	ms.Options = append(ms.Options, "create=file")
	if c.Spec.Root.Readonly {
		if err := os.MkdirAll(filepath.Dir(ms.Destination), 0755); err != nil {
			return fmt.Errorf("failed to create mount destination dir: %w", err)
		}
		f, err := os.OpenFile(ms.Destination, os.O_CREATE, 0755)
		if err != nil {
			return fmt.Errorf("failed to create file mountpoint: %w", err)
		}
		return f.Close()
	}
	return nil
}

func resolvePathRelative(rootfs string, currentPath string, subPath string) (string, error) {
	p := filepath.Join(currentPath, subPath)

	stat, err := os.Lstat(p)
	if err != nil {
		// target does not exist, resolution ends here
		return p, err
	}

	if stat.Mode()&os.ModeSymlink == 0 {
		return p, nil
	}
	// resolve symlink

	linkDst, err := os.Readlink(p)
	if err != nil {
		return p, err
	}

	// The destination of an absolute link must be prefixed with the rootfs
	if filepath.IsAbs(linkDst) {
		if filepath.HasPrefix(linkDst, rootfs) {
			return p, nil
		}
		return filepath.Join(rootfs, linkDst), nil
	}

	// The link target is relative to currentPath.
	return filepath.Clean(filepath.Join(currentPath, linkDst)), nil
}

// resolveMountDestination resolves mount destination paths for LXC.
//
// Symlinks in mount mount destination paths are not allowed in LXC.
// See CVE-2015-1335: Protect container mounts against symlinks
// and https://github.com/lxc/lxc/commit/592fd47a6245508b79fe6ac819fe6d3b2c1289be
// Mount targets that contain symlinks should be resolved relative to the container rootfs.
// e.g k8s service account tokens are mounted to /var/run/secrets/kubernetes.io/serviceaccount
// but /var/run is (mostly) a symlink to /run, so LXC denies to mount the serviceaccount token.
//
// The mount destination must be either relative to the container root or absolute to
// the directory on the host containing the rootfs.
// LXC simply ignores relative mounts paths to an absolute rootfs.
// See man lxc.container.conf #MOUNT POINTS
//
// The mount option `create=dir` should be set when the error os.ErrNotExist is returned.
// The non-existent directories are then automatically created by LXC.

// source /var/run/containers/storage/overlay-containers/51230afad17aa3b42901f6d9efcba406511821b7e18b2223a6b4c43f9327ce97/userdata/resolv.conf
// destination /etc/resolv.conf
func resolveMountDestination(rootfs string, dst string) (dstPath string, err error) {
	// get path entries
	entries := strings.Split(strings.TrimPrefix(dst, "/"), "/")

	currentPath := rootfs
	// start path resolution at rootfs
	for i, entry := range entries {
		currentPath, err = resolvePathRelative(rootfs, currentPath, entry)
		if err != nil {
			// The already resolved path is concatenated with the remaining path,
			// if resolution of path fails at some point.
			currentPath = filepath.Join(currentPath, filepath.Join(entries[i+1:]...))
			break
		}
	}
	return currentPath, err
}
