package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/creack/pty"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"

	lxc "gopkg.in/lxc/go-lxc.v2"
)

func doCreateInternal() error {
	err := canExecute(clxc.StartCommand, clxc.ContainerHook, clxc.InitCommand)
	if err != nil {
		return err
	}

	if err := isFilesystem("/proc", "proc"); err != nil {
		return err
	}
	if err := isFilesystem("/sys/fs/cgroup", "cgroup2"); err != nil {
		return err
	}
	// TODO test this version
	if !lxc.VersionAtLeast(4, 0, 5) {
		return fmt.Errorf("LXC runtime version >= 4.0.5 required, but was %s", lxc.Version())
	}

	err = clxc.createContainer()
	if err != nil {
		return errors.Wrap(err, "failed to create container")
	}

	if err := configureContainer(clxc.Spec); err != nil {
		return errors.Wrap(err, "failed to configure container")
	}

	// #nosec
	startCmd := exec.Command(clxc.StartCommand, clxc.Container.Name(), clxc.RuntimeRoot, clxc.ConfigFilePath())
	if err := runStartCmd(startCmd, clxc.Spec); err != nil {
		return errors.Wrap(err, "failed to start container process")
	}
	log.Info().Int("cpid", startCmd.Process.Pid).Int("pid", os.Getpid()).Int("ppid", os.Getppid()).Msg("started container process")

	if err := clxc.waitCreated(time.Second * 10); err != nil {
		log.Error().Int("cpid", startCmd.Process.Pid).Int("pid", os.Getpid()).Int("ppid", os.Getppid()).Msg("started container process")
		return err
	}
	return createPidFile(clxc.PidFile, startCmd.Process.Pid)
}

func configureContainer(spec *specs.Spec) error {
	if err := configureRootfs(spec); err != nil {
		return err
	}

	// pass context information as environment variables to hook scripts
	if err := clxc.setConfigItem("lxc.hook.version", "1"); err != nil {
		return err
	}
	if err := clxc.setConfigItem("lxc.hook.mount", clxc.ContainerHook); err != nil {
		return err
	}

	if err := configureInit(spec); err != nil {
		return err
	}

	if err := configureMounts(spec); err != nil {
		return err
	}

	if err := configureReadonlyPaths(spec); err != nil {
		return err
	}

	if err := configureNamespaces(spec.Linux.Namespaces); err != nil {
		return errors.Wrap(err, "failed to configure namespaces")
	}

	if spec.Process.OOMScoreAdj != nil {
		if err := clxc.setConfigItem("lxc.proc.oom_score_adj", fmt.Sprintf("%d", *spec.Process.OOMScoreAdj)); err != nil {
			return err
		}
	}

	if spec.Process.NoNewPrivileges {
		if err := clxc.setConfigItem("lxc.no_new_privs", "1"); err != nil {
			return err
		}
	}

	if clxc.Apparmor {
		if err := configureApparmor(spec); err != nil {
			return errors.Wrap(err, "failed to configure apparmor")
		}
	} else {
		log.Warn().Msg("apparmor is disabled (unconfined)")
	}

	if clxc.Seccomp {
		if err := configureSeccomp(spec); err != nil {
			return errors.Wrap(err, "failed to configure seccomp")
		}
	} else {
		log.Warn().Msg("seccomp is disabled")
	}

	if clxc.Capabilities {
		if err := configureCapabilities(spec); err != nil {
			return errors.Wrap(err, "failed to configure capabilities")
		}
	} else {
		log.Warn().Msg("capabilities are disabled")
	}

	if err := setHostname(spec); err != nil {
		return errors.Wrap(err, "set hostname")
	}

	if err := ensureDefaultDevices(spec); err != nil {
		return errors.Wrap(err, "failed to add default devices")
	}

	if err := clxc.configureCgroupPath(); err != nil {
		return errors.Wrap(err, "failed to configure cgroups path")
	}

	if err := configureCgroup(spec); err != nil {
		return errors.Wrap(err, "failed to configure cgroups")
	}

	for key, val := range spec.Linux.Sysctl {
		if err := clxc.setConfigItem("lxc.sysctl."+key, val); err != nil {
			return err
		}
	}

	for _, limit := range spec.Process.Rlimits {
		name := strings.TrimPrefix(strings.ToLower(limit.Type), "rlimit_")
		val := fmt.Sprintf("%d:%d", limit.Soft, limit.Hard)
		if err := clxc.setConfigItem("lxc.prlimit."+name, val); err != nil {
			return err
		}
	}
	return nil
}

func configureRootfs(spec *specs.Spec) error {
	if err := clxc.setConfigItem("lxc.rootfs.path", spec.Root.Path); err != nil {
		return err
	}
	if err := clxc.setConfigItem("lxc.rootfs.managed", "0"); err != nil {
		return err
	}

	// Resources not created by the container runtime MUST NOT be deleted by it.
	if err := clxc.setConfigItem("lxc.ephemeral", "0"); err != nil {
		return err
	}

	rootfsOptions := []string{}
	if spec.Linux.RootfsPropagation != "" {
		rootfsOptions = append(rootfsOptions, spec.Linux.RootfsPropagation)
	}
	if spec.Root.Readonly {
		rootfsOptions = append(rootfsOptions, "ro")
	}
	if err := clxc.setConfigItem("lxc.rootfs.options", strings.Join(rootfsOptions, ",")); err != nil {
		return err
	}
	return nil
}

func configureReadonlyPaths(spec *specs.Spec) error {
	rootmnt := clxc.getConfigItem("lxc.rootfs.mount")
	if rootmnt == "" {
		return errors.New("lxc.rootfs.mount unavailable")
	}
	for _, p := range spec.Linux.ReadonlyPaths {
		mnt := fmt.Sprintf("%s %s %s %s", filepath.Join(rootmnt, p), strings.TrimPrefix(p, "/"), "bind", "bind,ro,optional")
		if err := clxc.setConfigItem("lxc.mount.entry", mnt); err != nil {
			return errors.Wrap(err, "failed to make path readonly")
		}
	}
	return nil
}

func configureApparmor(spec *specs.Spec) error {
	// The value *apparmor_profile*  from crio.conf is used if no profile is defined by the container.
	aaprofile := spec.Process.ApparmorProfile
	if aaprofile == "" {
		aaprofile = "unconfined"
	}
	return clxc.setConfigItem("lxc.apparmor.profile", aaprofile)
}

// configureCapabilities configures the linux capabilities / privileges granted to the container processes.
// See `man lxc.container.conf` lxc.cap.drop and lxc.cap.keep for details.
// https://blog.container-solutions.com/linux-capabilities-in-practice
// https://blog.container-solutions.com/linux-capabilities-why-they-exist-and-how-they-work
func configureCapabilities(spec *specs.Spec) error {
	keepCaps := "none"
	if spec.Process.Capabilities != nil {
		var caps []string
		for _, c := range spec.Process.Capabilities.Permitted {
			lcCapName := strings.TrimPrefix(strings.ToLower(c), "cap_")
			caps = append(caps, lcCapName)
		}
		keepCaps = strings.Join(caps, " ")
	}

	return clxc.setConfigItem("lxc.cap.keep", keepCaps)
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
func ensureDefaultDevices(spec *specs.Spec) error {
	// make sure autodev is disabled
	if err := clxc.setConfigItem("lxc.autodev", "0"); err != nil {
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

func runStartCmd(cmd *exec.Cmd, spec *specs.Spec) error {
	// Start container with a clean environment.
	// LXC will export variables defined in the config lxc.environment.
	// The environment variables defined by the container spec are exported within the init cmd CRIO_LXC_INIT_CMD.
	// This is required because environment variables defined by containers contain newlines and other tokens
	// that can not be handled properly within the lxc config file.
	cmd.Env = []string{}
	/*
		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = &unix.SysProcAttr{}
		}

		//cmd.SysProcAttr.Noctty = true
		sig, err := getParentDeathSignal()
		if err != nil {
			return err
		}
		cmd.SysProcAttr.Pdeathsig = sig
		//cmd.SysProcAttr.Foreground = false
		//cmd.SysProcAttr.Setsid = true
	*/

	cmd.Dir = clxc.RuntimePath()

	if clxc.ConsoleSocket != "" {
		if err := clxc.saveConfig(); err != nil {
			return err
		}
		return runStartCmdConsole(cmd, clxc.ConsoleSocket)
	}
	if !spec.Process.Terminal {
		// Inherit stdio from calling process (conmon).
		// lxc.console.path must be set to 'none' or stdio of init process is replaced with a PTY by lxc
		if err := clxc.setConfigItem("lxc.console.path", "none"); err != nil {
			return errors.Wrap(err, "failed to disable PTY")
		}
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := clxc.saveConfig(); err != nil {
		return err
	}

	if err := writeDevices(spec); err != nil {
		return errors.Wrap(err, "failed to create devices.txt")
	}

	if err := writeMasked(spec); err != nil {
		return errors.Wrap(err, "failed to create masked.txt file")
	}

	return cmd.Start()
}

func writeDevices(spec *specs.Spec) error {
	if spec.Linux.Devices == nil {
		return nil
	}
	f, err := os.OpenFile(clxc.RuntimePath("devices.txt"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	for _, d := range spec.Linux.Devices {
		uid := spec.Process.User.UID // FIXME use 0 instead ?
		if d.UID != nil {
			uid = *d.UID
		}
		gid := spec.Process.User.GID // FIXME use 0 instead ?
		if d.GID == nil {
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

func writeMasked(spec *specs.Spec) error {
	// #nosec
	if spec.Linux.MaskedPaths == nil {
		return nil
	}
	f, err := os.OpenFile(clxc.RuntimePath("masked.txt"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
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

func runStartCmdConsole(cmd *exec.Cmd, consoleSocket string) error {
	addr, err := net.ResolveUnixAddr("unix", consoleSocket)
	if err != nil {
		return errors.Wrap(err, "failed to resolve console socket")
	}
	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		return errors.Wrap(err, "connecting to console socket failed")
	}
	defer conn.Close()
	deadline := time.Now().Add(clxc.ConsoleSocketTimeout)
	err = conn.SetDeadline(deadline)
	if err != nil {
		return errors.Wrap(err, "failed to set connection deadline")
	}

	sockFile, err := conn.File()
	if err != nil {
		return errors.Wrap(err, "failed to get file from unix connection")
	}
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return errors.Wrap(err, "failed to start with pty")
	}

	// Send the pty file descriptor over the console socket (to the 'conmon' process)
	// For technical backgrounds see:
	// * `man sendmsg 2`, `man unix 3`, `man cmsg 1`
	// * https://blog.cloudflare.com/know-your-scm_rights/
	oob := unix.UnixRights(int(ptmx.Fd()))
	// Don't know whether 'terminal' is the right data to send, but conmon doesn't care anyway.
	err = unix.Sendmsg(int(sockFile.Fd()), []byte("terminal"), oob, nil, 0)
	if err != nil {
		return errors.Wrap(err, "failed to send console fd")
	}
	return ptmx.Close()
}

func setHostname(spec *specs.Spec) error {
	if spec.Hostname == "" {
		return nil
	}

	if err := clxc.setConfigItem("lxc.uts.name", spec.Hostname); err != nil {
		return err
	}

	// lxc does not set the hostname on shared namespaces
	for _, ns := range spec.Linux.Namespaces {
		if ns.Type != specs.UTSNamespace {
			continue
		}

		// namespace is not shared
		if ns.Path == "" {
			return nil
		}

		// setns only affects the current thread
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		f, err := os.Open(ns.Path)
		if err != nil {
			return errors.Wrapf(err, "failed to open uts namespace %s", ns.Path)
		}
		// #nosec
		defer f.Close()

		self, err := os.Open("/proc/self/ns/uts")
		if err != nil {
			return errors.Wrapf(err, "failed to open %s", "/proc/self/ns/uts")
		}
		// #nosec
		defer self.Close()

		err = unix.Setns(int(f.Fd()), unix.CLONE_NEWUTS)
		if err != nil {
			return err
		}
		err = unix.Sethostname([]byte(spec.Hostname))
		if err != nil {
			return err
		}
		err = unix.Setns(int(self.Fd()), unix.CLONE_NEWUTS)
		if err != nil {
			return err
		}
	}
	return nil
}
