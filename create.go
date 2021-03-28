package lxcri

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
	"gopkg.in/lxc/go-lxc.v2"
)

// Create creates a single container instance from the given ContainerConfig.
// Create is the first runtime method to call within the lifecycle of a container.
// You may have to call Runtime.Delete to cleanup container runtime state,
// if Create returns with an error.
func (rt *Runtime) Create(ctx context.Context, cfg *ContainerConfig) (*Container, error) {
	ctx, cancel := context.WithTimeout(ctx, rt.Timeouts.Create)
	defer cancel()

	if err := rt.checkConfig(cfg); err != nil {
		return nil, err
	}

	c := &Container{ContainerConfig: cfg}
	c.RuntimeDir = filepath.Join(rt.Root, c.ContainerID)

	if err := c.create(); err != nil {
		return c, errorf("failed to create container: %w", err)
	}

	if rt.OnCreate != nil {
		rt.OnCreate(ctx, c)
	}
	if c.OnCreate != nil {
		c.OnCreate(ctx, c)
	}

	if err := configureContainer(rt, c); err != nil {
		return c, errorf("failed to configure container: %w", err)
	}

	if err := rt.runStartCmd(ctx, c); err != nil {
		return c, errorf("failed to run container process: %w", err)
	}

	return c, nil
}

// CheckSystem checks the hosts system configuration.
// Unsupported runtime features are disabled and a warning message is logged.
// CheckSystem should be called (once) before using the Runtime.
func (rt *Runtime) CheckSystem() error {
	err := canExecute(rt.libexec(ExecStart), rt.libexec(ExecHook), rt.libexec(ExecInit))
	if err != nil {
		return errorf("access check failed: %w", err)
	}

	if err := isFilesystem("/proc", "proc"); err != nil {
		return errorf("procfs not mounted on /proc: %w", err)
	}
	if err := isFilesystem(cgroupRoot, "cgroup2"); err != nil {
		return errorf("ccgroup2 not mounted on %s: %w", cgroupRoot, err)
	}

	if !lxc.VersionAtLeast(3, 1, 0) {
		return errorf("liblxc runtime version is %s, but >= 3.1.0 is required", lxc.Version())
	}

	if !lxc.VersionAtLeast(4, 0, 5) {
		rt.Log.Warn().Msgf("liblxc runtime version >= 4.0.5 is recommended (was %s)", lxc.Version())
	}

	return nil
}

func (rt *Runtime) checkConfig(config *ContainerConfig) error {
	if config.Linux.Resources == nil {
		config.Linux.Resources = &specs.LinuxResources{}
	}

	if config.Linux.Devices == nil {
		config.Linux.Devices = make([]specs.LinuxDevice, 0, 20)
	}

	if config.Process == nil {
		return fmt.Errorf("config.Process is nil")
	}

	if len(config.Process.Args) == 0 {
		return fmt.Errorf("configs.Process.Args is empty")
	}

	if config.Process.Cwd == "" {
		rt.Log.Info().Msg("configs.Process.Cwd is unset defaulting to '/'")
		config.Process.Cwd = "/"
	}
	return nil
}

func configureContainer(rt *Runtime, c *Container) error {
	if c.Hostname != "" {
		if err := c.SetConfigItem("lxc.uts.name", c.Hostname); err != nil {
			return err
		}

		uts := getNamespace(specs.UTSNamespace, c.Linux.Namespaces)
		if uts != nil && uts.Path != "" {
			if err := setHostname(uts.Path, c.Hostname); err != nil {
				return fmt.Errorf("failed  to set hostname: %w", err)
			}
		}
	}

	if err := configureRootfs(c); err != nil {
		return err
	}

	if err := configureInit(rt, c); err != nil {
		return err
	}

	if err := configureMounts(rt, c); err != nil {
		return err
	}

	if err := configureReadonlyPaths(c); err != nil {
		return err
	}

	if err := configureNamespaces(c); err != nil {
		return fmt.Errorf("failed to configure namespaces: %w", err)
	}

	if c.Process.OOMScoreAdj != nil {
		if err := c.SetConfigItem("lxc.proc.oom_score_adj", fmt.Sprintf("%d", *c.Process.OOMScoreAdj)); err != nil {
			return err
		}
	}

	if c.Process.NoNewPrivileges {
		if err := c.SetConfigItem("lxc.no_new_privs", "1"); err != nil {
			return err
		}
	}

	if rt.Features.Apparmor {
		if err := configureApparmor(c); err != nil {
			return fmt.Errorf("failed to configure apparmor: %w", err)
		}
	} else {
		rt.Log.Warn().Msg("apparmor feature is disabled - profile is set to unconfined")
	}

	if rt.Features.Seccomp {
		if c.Linux.Seccomp != nil && len(c.Linux.Seccomp.Syscalls) > 0 {
			profilePath := c.RuntimePath("seccomp.conf")
			if err := writeSeccompProfile(profilePath, c.Linux.Seccomp); err != nil {
				return err
			}
			if err := c.SetConfigItem("lxc.seccomp.profile", profilePath); err != nil {
				return err
			}
		}
	} else {
		rt.Log.Warn().Msg("seccomp feature is disabled - all system calls are allowed")
	}

	if rt.Features.Capabilities {
		if err := configureCapabilities(c); err != nil {
			return fmt.Errorf("failed to configure capabilities: %w", err)
		}
	} else {
		rt.Log.Warn().Msg("capabilities feature is disabled - running with full privileges")
	}

	// make sure autodev is disabled
	if err := c.SetConfigItem("lxc.autodev", "0"); err != nil {
		return err
	}
	if err := ensureDefaultDevices(c); err != nil {
		return fmt.Errorf("failed to add default devices: %w", err)
	}

	if err := writeDevices(c.RuntimePath("devices.txt"), c); err != nil {
		return fmt.Errorf("failed to create devices.txt: %w", err)
	}

	if err := writeMasked(c.RuntimePath("masked.txt"), c); err != nil {
		return fmt.Errorf("failed to create masked.txt: %w", err)
	}

	// pass context information as environment variables to hook scripts
	if err := c.SetConfigItem("lxc.hook.version", "1"); err != nil {
		return err
	}
	if err := c.SetConfigItem("lxc.hook.mount", rt.libexec(ExecHook)); err != nil {
		return err
	}

	if err := configureCgroup(rt, c); err != nil {
		return fmt.Errorf("failed to configure cgroups: %w", err)
	}

	for key, val := range c.Linux.Sysctl {
		if err := c.SetConfigItem("lxc.sysctl."+key, val); err != nil {
			return err
		}
	}

	// `man lxc.container.conf`: "A resource with no explicitly configured limitation will be inherited
	// from the process starting up the container"
	seenLimits := make([]string, 0, len(c.Process.Rlimits))
	for _, limit := range c.Process.Rlimits {
		name := strings.TrimPrefix(strings.ToLower(limit.Type), "rlimit_")
		for _, seen := range seenLimits {
			if seen == name {
				return fmt.Errorf("duplicate resource limit %q", limit.Type)
			}
		}
		seenLimits = append(seenLimits, name)
		val := fmt.Sprintf("%d:%d", limit.Soft, limit.Hard)
		if err := c.SetConfigItem("lxc.prlimit."+name, val); err != nil {
			return err
		}
	}

	if err := setLog(c); err != nil {
		return errorf("failed to configure container log: %w", err)
	}

	return nil
}

func configureRootfs(c *Container) error {
	if err := c.SetConfigItem("lxc.rootfs.path", c.Root.Path); err != nil {
		return err
	}
	if err := c.SetConfigItem("lxc.rootfs.managed", "0"); err != nil {
		return err
	}

	// Resources not created by the container runtime MUST NOT be deleted by it.
	if err := c.SetConfigItem("lxc.ephemeral", "0"); err != nil {
		return err
	}

	rootfsOptions := []string{}
	if c.Linux.RootfsPropagation != "" {
		rootfsOptions = append(rootfsOptions, c.Linux.RootfsPropagation)
	}
	if c.Root.Readonly {
		rootfsOptions = append(rootfsOptions, "ro")
	}
	if err := c.SetConfigItem("lxc.rootfs.options", strings.Join(rootfsOptions, ",")); err != nil {
		return err
	}
	return nil
}

func configureReadonlyPaths(c *Container) error {
	rootmnt := c.GetConfigItem("lxc.rootfs.mount")
	if rootmnt == "" {
		return fmt.Errorf("lxc.rootfs.mount unavailable")
	}
	for _, p := range c.Linux.ReadonlyPaths {
		mnt := fmt.Sprintf("%s %s %s %s", filepath.Join(rootmnt, p), strings.TrimPrefix(p, "/"), "bind", "bind,ro,optional")
		if err := c.SetConfigItem("lxc.mount.entry", mnt); err != nil {
			return fmt.Errorf("failed to make path readonly: %w", err)
		}
	}
	return nil
}

func configureApparmor(c *Container) error {
	// The value *apparmor_profile*  from crio.conf is used if no profile is defined by the container.
	aaprofile := c.Process.ApparmorProfile
	if aaprofile == "" {
		aaprofile = "unconfined"
	}
	return c.SetConfigItem("lxc.apparmor.profile", aaprofile)
}

// configureCapabilities configures the linux capabilities / privileges granted to the container processes.
// See `man lxc.container.conf` lxc.cap.drop and lxc.cap.keep for details.
// https://blog.container-solutions.com/linux-capabilities-in-practice
// https://blog.container-solutions.com/linux-capabilities-why-they-exist-and-how-they-work
func configureCapabilities(c *Container) error {
	keepCaps := "none"
	if c.Process.Capabilities != nil {
		var caps []string
		for _, c := range c.Process.Capabilities.Permitted {
			lcCapName := strings.TrimPrefix(strings.ToLower(c), "cap_")
			caps = append(caps, lcCapName)
		}
		if len(caps) > 0 {
			keepCaps = strings.Join(caps, " ")
		}
	}

	return c.SetConfigItem("lxc.cap.keep", keepCaps)
}

func writeMasked(dst string, c *Container) error {
	// #nosec
	if c.Linux.MaskedPaths == nil {
		return nil
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	for _, p := range c.Linux.MaskedPaths {
		_, err = fmt.Fprintln(f, p)
		if err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}
