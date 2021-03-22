package lxcontainer

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/creack/pty"
	"github.com/opencontainers/runtime-spec/specs-go"
	"gopkg.in/lxc/go-lxc.v2"
)

func (c *Runtime) Create(ctx context.Context, spec *specs.Spec) error {
	ctx, cancel := context.WithTimeout(ctx, c.CreateTimeout)
	defer cancel()

	if c.runtimePathExists() {
		return ErrExist
	}

	err := canExecute(c.StartCommand, c.ContainerHook, c.InitCommand)
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
		c.Log.Warn().Msgf("liblxc runtime version >= 4.0.5 is recommended (was %s)", lxc.Version())
	}

	if spec.Linux.Resources == nil {
		spec.Linux.Resources = &specs.LinuxResources{}
	}

	if spec.Linux.Devices == nil {
		spec.Linux.Devices = make([]specs.LinuxDevice, 0, 20)
	}

	if spec.Process == nil {
		return fmt.Errorf("spec.Process is nil")
	}

	if len(spec.Process.Args) == 0 {
		return fmt.Errorf("specs.Process.Args is empty")
	}

	if spec.Process.Cwd == "" {
		c.Log.Info().Msg("specs.Process.Cwd is unset defaulting to '/'")
		spec.Process.Cwd = "/"
	}

	err = c.createContainer(spec)
	if err != nil {
		return errorf("failed to create container: %w", err)
	}

	if err := configureContainer(c, spec); err != nil {
		return errorf("failed to configure container: %w", err)
	}

	if err := c.runStartCmd(ctx, spec); err != nil {
		return errorf("failed to run container process: %w", err)
	}
	return nil
}

func (c *Runtime) runStartCmd(ctx context.Context, spec *specs.Spec) (err error) {
	// #nosec
	cmd := exec.Command(c.StartCommand, c.Container.Name(), c.RuntimeRoot, c.ConfigFilePath())
	cmd.Env = []string{}
	cmd.Dir = c.RuntimePath()

	if c.ConsoleSocket == "" && !spec.Process.Terminal {
		// Inherit stdio from calling process (conmon).
		// lxc.console.path must be set to 'none' or stdio of init process is replaced with a PTY by lxc
		if err := c.setConfigItem("lxc.console.path", "none"); err != nil {
			return err
		}
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := c.saveConfig(); err != nil {
		return err
	}

	c.Log.Debug().Msg("starting lxc monitor process")
	if c.ConsoleSocket != "" {
		err = runStartCmdConsole(ctx, cmd, c.ConsoleSocket)
	} else {
		err = cmd.Start()
	}

	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		// NOTE this goroutine may leak until crio-lxc is terminated
		ps, err := cmd.Process.Wait()
		if err != nil {
			c.Log.Error().Err(err).Msg("failed to wait for start process")
		} else {
			c.Log.Warn().Int("pid", cmd.Process.Pid).Stringer("status", ps).Msg("start process terminated")
		}
		cancel()
	}()

	c.Log.Debug().Msg("waiting for init")
	if err := c.waitCreated(ctx); err != nil {
		return err
	}

	c.Log.Info().Int("pid", cmd.Process.Pid).Msg("init process is running, container is created")
	return CreatePidFile(c.PidFile, cmd.Process.Pid)
}

func configureContainer(c *Runtime, spec *specs.Spec) error {
	if spec.Hostname != "" {
		if err := c.setConfigItem("lxc.uts.name", spec.Hostname); err != nil {
			return err
		}

		uts := getNamespace(specs.UTSNamespace, spec.Linux.Namespaces)
		if uts != nil && uts.Path != "" {
			if err := setHostname(uts.Path, spec.Hostname); err != nil {
				return fmt.Errorf("failed  to set hostname: %w", err)
			}
		}
	}

	if err := configureRootfs(c, spec); err != nil {
		return err
	}

	if err := configureInit(c, spec); err != nil {
		return err
	}

	if err := configureMounts(c, spec); err != nil {
		return err
	}

	if err := configureReadonlyPaths(c, spec); err != nil {
		return err
	}

	if err := configureNamespaces(c, spec.Linux.Namespaces); err != nil {
		return fmt.Errorf("failed to configure namespaces: %w", err)
	}

	if spec.Process.OOMScoreAdj != nil {
		if err := c.setConfigItem("lxc.proc.oom_score_adj", fmt.Sprintf("%d", *spec.Process.OOMScoreAdj)); err != nil {
			return err
		}
	}

	if spec.Process.NoNewPrivileges {
		if err := c.setConfigItem("lxc.no_new_privs", "1"); err != nil {
			return err
		}
	}

	if c.Features.Apparmor {
		if err := configureApparmor(c, spec); err != nil {
			return fmt.Errorf("failed to configure apparmor: %w", err)
		}
	} else {
		c.Log.Warn().Msg("apparmor feature is disabled - profile is set to unconfined")
	}

	if c.Features.Seccomp {
		if spec.Linux.Seccomp != nil && len(spec.Linux.Seccomp.Syscalls) > 0 {
			profilePath := c.RuntimePath("seccomp.conf")
			if err := writeSeccompProfile(profilePath, spec.Linux.Seccomp); err != nil {
				return err
			}
			if err := c.setConfigItem("lxc.seccomp.profile", profilePath); err != nil {
				return err
			}
		}
	} else {
		c.Log.Warn().Msg("seccomp feature is disabled - all system calls are allowed")
	}

	if c.Features.Capabilities {
		if err := configureCapabilities(c, spec); err != nil {
			return fmt.Errorf("failed to configure capabilities: %w", err)
		}
	} else {
		c.Log.Warn().Msg("capabilities feature is disabled - running with full privileges")
	}

	if err := ensureDefaultDevices(c, spec); err != nil {
		return fmt.Errorf("failed to add default devices: %w", err)
	}

	if err := writeDevices(c.RuntimePath("devices.txt"), spec); err != nil {
		return fmt.Errorf("failed to create devices.txt: %w", err)
	}

	if err := writeMasked(c.RuntimePath("masked.txt"), spec); err != nil {
		return fmt.Errorf("failed to create masked.txt: %w", err)
	}

	// pass context information as environment variables to hook scripts
	if err := c.setConfigItem("lxc.hook.version", "1"); err != nil {
		return err
	}
	if err := c.setConfigItem("lxc.hook.mount", c.ContainerHook); err != nil {
		return err
	}

	if err := c.configureCgroupPath(); err != nil {
		return fmt.Errorf("failed to configure cgroups path: %w", err)
	}

	if err := configureCgroup(c, spec); err != nil {
		return fmt.Errorf("failed to configure cgroups: %w", err)
	}

	for key, val := range spec.Linux.Sysctl {
		if err := c.setConfigItem("lxc.sysctl."+key, val); err != nil {
			return err
		}
	}

	// `man lxc.container.conf`: "A resource with no explicitly configured limitation will be inherited
	// from the process starting up the container"
	seenLimits := make([]string, 0, len(spec.Process.Rlimits))
	for _, limit := range spec.Process.Rlimits {
		name := strings.TrimPrefix(strings.ToLower(limit.Type), "rlimit_")
		for _, seen := range seenLimits {
			if seen == name {
				return fmt.Errorf("duplicate resource limit %q", limit.Type)
			}
		}
		seenLimits = append(seenLimits, name)
		val := fmt.Sprintf("%d:%d", limit.Soft, limit.Hard)
		if err := c.setConfigItem("lxc.prlimit."+name, val); err != nil {
			return err
		}
	}
	return nil
}

func configureRootfs(c *Runtime, spec *specs.Spec) error {
	if err := c.setConfigItem("lxc.rootfs.path", spec.Root.Path); err != nil {
		return err
	}
	if err := c.setConfigItem("lxc.rootfs.managed", "0"); err != nil {
		return err
	}

	// Resources not created by the container runtime MUST NOT be deleted by it.
	if err := c.setConfigItem("lxc.ephemeral", "0"); err != nil {
		return err
	}

	rootfsOptions := []string{}
	if spec.Linux.RootfsPropagation != "" {
		rootfsOptions = append(rootfsOptions, spec.Linux.RootfsPropagation)
	}
	if spec.Root.Readonly {
		rootfsOptions = append(rootfsOptions, "ro")
	}
	if err := c.setConfigItem("lxc.rootfs.options", strings.Join(rootfsOptions, ",")); err != nil {
		return err
	}
	return nil
}

func configureReadonlyPaths(c *Runtime, spec *specs.Spec) error {
	rootmnt := c.getConfigItem("lxc.rootfs.mount")
	if rootmnt == "" {
		return fmt.Errorf("lxc.rootfs.mount unavailable")
	}
	for _, p := range spec.Linux.ReadonlyPaths {
		mnt := fmt.Sprintf("%s %s %s %s", filepath.Join(rootmnt, p), strings.TrimPrefix(p, "/"), "bind", "bind,ro,optional")
		if err := c.setConfigItem("lxc.mount.entry", mnt); err != nil {
			return fmt.Errorf("failed to make path readonly: %w", err)
		}
	}
	return nil
}

func configureApparmor(c *Runtime, spec *specs.Spec) error {
	// The value *apparmor_profile*  from crio.conf is used if no profile is defined by the container.
	aaprofile := spec.Process.ApparmorProfile
	if aaprofile == "" {
		aaprofile = "unconfined"
	}
	return c.setConfigItem("lxc.apparmor.profile", aaprofile)
}

// configureCapabilities configures the linux capabilities / privileges granted to the container processes.
// See `man lxc.container.conf` lxc.cap.drop and lxc.cap.keep for details.
// https://blog.container-solutions.com/linux-capabilities-in-practice
// https://blog.container-solutions.com/linux-capabilities-why-they-exist-and-how-they-work
func configureCapabilities(c *Runtime, spec *specs.Spec) error {
	keepCaps := "none"
	if spec.Process.Capabilities != nil {
		var caps []string
		for _, c := range spec.Process.Capabilities.Permitted {
			lcCapName := strings.TrimPrefix(strings.ToLower(c), "cap_")
			caps = append(caps, lcCapName)
		}
		if len(caps) > 0 {
			keepCaps = strings.Join(caps, " ")
		}
	}

	return c.setConfigItem("lxc.cap.keep", keepCaps)
}

func isDeviceEnabled(spec *specs.Spec, dev specs.LinuxDevice) bool {
	for _, specDev := range spec.Linux.Devices {
		if specDev.Path == dev.Path {
			return true
		}
	}
	return false
}

func addDevice(spec *specs.Spec, dev specs.LinuxDevice, mode os.FileMode, uid uint32, gid uint32, access string) {
	dev.FileMode = &mode
	dev.UID = &uid
	dev.GID = &gid
	spec.Linux.Devices = append(spec.Linux.Devices, dev)

	addDevicePerms(spec, dev.Type, &dev.Major, &dev.Minor, access)
}

func addDevicePerms(spec *specs.Spec, devType string, major *int64, minor *int64, access string) {
	devCgroup := specs.LinuxDeviceCgroup{Allow: true, Type: devType, Major: major, Minor: minor, Access: access}
	spec.Linux.Resources.Devices = append(spec.Linux.Resources.Devices, devCgroup)
}

// ensureDefaultDevices adds the mandatory devices defined by the [runtime spec](https://github.com/opencontainers/runtime-spec/blob/master/config-linux.md#default-devices)
// to the given container spec if required.
// crio can add devices to containers, but this does not work for privileged containers.
// See https://github.com/cri-o/cri-o/blob/a705db4c6d04d7c14a4d59170a0ebb4b30850675/server/container_create_linux.go#L45
// TODO file an issue on cri-o (at least for support)
func ensureDefaultDevices(c *Runtime, spec *specs.Spec) error {
	// make sure autodev is disabled
	if err := c.setConfigItem("lxc.autodev", "0"); err != nil {
		return err
	}

	mode := os.FileMode(0666)
	var uid, gid uint32 = spec.Process.User.UID, spec.Process.User.GID

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
	addDevicePerms(spec, "c", &ptmx.Major, &ptmx.Minor, "rwm") // /dev/ptmx, /dev/pts/ptmx

	pts0 := specs.LinuxDevice{Path: "/dev/pts/0", Type: "c", Major: 88, Minor: 0}
	addDevicePerms(spec, "c", &pts0.Major, nil, "rwm") // dev/pts/[0..9]

	if spec.Linux.Resources == nil {
		spec.Linux.Resources = &specs.LinuxResources{}
	}

	// add missing default devices
	for _, dev := range devices {
		if !isDeviceEnabled(spec, dev) {
			addDevice(spec, dev, mode, uid, gid, "rwm")
		}
	}
	return nil
}

func setenv(env []string, key, val string, overwrite bool) []string {
	for i, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			if overwrite {
				env[i] = key + "=" + val
			}
			return env
		}
	}
	return append(env, key+"="+val)
}

func writeDevices(dst string, spec *specs.Spec) error {
	if spec.Linux.Devices == nil {
		return nil
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	for _, d := range spec.Linux.Devices {
		uid := spec.Process.User.UID
		if d.UID != nil {
			uid = *d.UID
		}
		gid := spec.Process.User.GID
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

func writeMasked(dst string, spec *specs.Spec) error {
	// #nosec
	if spec.Linux.MaskedPaths == nil {
		return nil
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	for _, p := range spec.Linux.MaskedPaths {
		_, err = fmt.Fprintln(f, p)
		if err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

func runStartCmdConsole(ctx context.Context, cmd *exec.Cmd, consoleSocket string) error {
	dialer := net.Dialer{}
	c, err := dialer.DialContext(ctx, "unix", consoleSocket)
	if err != nil {
		return fmt.Errorf("connecting to console socket failed: %w", err)
	}
	defer c.Close()

	conn, ok := c.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("expected a unix connection but was %T", conn)
	}

	if deadline, ok := ctx.Deadline(); ok {
		err = conn.SetDeadline(deadline)
		if err != nil {
			return fmt.Errorf("failed to set connection deadline: %w", err)
		}
	}

	sockFile, err := conn.File()
	if err != nil {
		return fmt.Errorf("failed to get file from unix connection: %w", err)
	}
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start with pty: %w", err)
	}

	// Send the pty file descriptor over the console socket (to the 'conmon' process)
	// For technical backgrounds see:
	// * `man sendmsg 2`, `man unix 3`, `man cmsg 1`
	// * https://blog.cloudflare.com/know-your-scm_rights/
	oob := unix.UnixRights(int(ptmx.Fd()))
	// Don't know whether 'terminal' is the right data to send, but conmon doesn't care anyway.
	err = unix.Sendmsg(int(sockFile.Fd()), []byte("terminal"), oob, nil, 0)
	if err != nil {
		return fmt.Errorf("failed to send console fd: %w", err)
	}
	return ptmx.Close()
}
