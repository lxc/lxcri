package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/creack/pty"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"

	"github.com/lxc/crio-lxc/cmd/internal"
	lxc "gopkg.in/lxc/go-lxc.v2"
)

var createCmd = cli.Command{
	Name:      "create",
	Usage:     "create a container from a bundle directory",
	ArgsUsage: "<containerID>",
	Action:    doCreate,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:        "bundle",
			Usage:       "set bundle directory",
			Value:       ".",
			Destination: &clxc.BundlePath,
		},
		&cli.StringFlag{
			Name:        "console-socket",
			Usage:       "send container pty master fd to this socket path",
			Destination: &clxc.ConsoleSocket,
		},
		&cli.StringFlag{
			Name:        "pid-file",
			Usage:       "path to write container PID",
			Destination: &clxc.PidFile,
		},
		&cli.DurationFlag{
			Name:        "timeout",
			Usage:       "timeout for container creation",
			EnvVars:     []string{"CRIO_LXC_CREATE_TIMEOUT"},
			Value:       time.Second * 5,
			Destination: &clxc.CreateTimeout,
		},
	},
}

func doCreate(ctx *cli.Context) error {
	err := doCreateInternal()
	if clxc.Backup || (err != nil && clxc.BackupOnError) {
		backupDir, backupErr := clxc.backupRuntimeResources()
		if backupErr == nil {
			log.Warn().Str("dir:", backupDir).Msg("runtime backup completed")
		} else {
			log.Error().Err(backupErr).Str("dir:", backupDir).Msg("runtime backup failed")
		}
	}
	return err
}

func doCreateInternal() error {
	// minimal lxc version is 3.1 https://discuss.linuxcontainers.org/t/lxc-3-1-has-been-released/3527
	if !lxc.VersionAtLeast(3, 1, 0) {
		return fmt.Errorf("LXC runtime version > 3.1.0 required, but was %s", lxc.Version())
	}

	err := clxc.loadContainer()
	if err == nil {
		return fmt.Errorf("container already exists")
	}

	err = clxc.createContainer()
	if err != nil {
		return errors.Wrap(err, "failed to create container")
	}

	if err := clxc.setConfigItem("lxc.log.file", clxc.LogFilePath); err != nil {
		return err
	}

	err = clxc.Container.SetLogLevel(clxc.LogLevel)
	if err != nil {
		return errors.Wrap(err, "failed to set container loglevel")
	}
	if clxc.LogLevel == lxc.TRACE {
		clxc.Container.SetVerbosity(lxc.Verbose)
	}

	clxc.SpecPath = filepath.Join(clxc.BundlePath, "config.json")
	spec, err := internal.ReadSpec(clxc.SpecPath)
	if err != nil {
		return errors.Wrap(err, "couldn't load bundle spec")
	}

	if err := configureContainer(spec); err != nil {
		return errors.Wrap(err, "failed to configure container")
	}
	return startContainer(spec, clxc.CreateTimeout)
}

func configureContainer(spec *specs.Spec) error {
	if err := configureRootfs(spec); err != nil {
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

	// TODO extract all uid/gid related settings into separate function configureUserAndGroups
	if err := clxc.setConfigItem("lxc.init.uid", fmt.Sprintf("%d", spec.Process.User.UID)); err != nil {
		return err
	}
	if err := clxc.setConfigItem("lxc.init.gid", fmt.Sprintf("%d", spec.Process.User.GID)); err != nil {
		return err
	}

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

	if len(spec.Process.User.AdditionalGids) > 0 && supportsConfigItem("lxc.init.groups") {
		a := make([]string, 0, len(spec.Process.User.AdditionalGids))
		for _, gid := range spec.Process.User.AdditionalGids {
			a = append(a, strconv.FormatUint(uint64(gid), 10))
		}
		if err := clxc.setConfigItem("lxc.init.groups", strings.Join(a, " ")); err != nil {
			return err
		}
	}

	if err := setHostname(spec); err != nil {
		return errors.Wrap(err, "set hostname")
	}

	if err := ensureDefaultDevices(spec); err != nil {
		return errors.Wrap(err, "failed to add default devices")
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

func configureInit(spec *specs.Spec) error {
	err := os.MkdirAll(filepath.Join(spec.Root.Path, internal.ConfigDir), 0)
	if err != nil {
		return errors.Wrapf(err, "Failed creating %s in rootfs", internal.ConfigDir)
	}
	err = os.MkdirAll(clxc.runtimePath(internal.ConfigDir), 0755)
	if err != nil {
		return errors.Wrapf(err, "Failed creating %s in lxc container dir", internal.ConfigDir)
	}

	// create named fifo in lxcpath and mount it into the container
	if err := makeSyncFifo(clxc.runtimePath(internal.SyncFifoPath)); err != nil {
		return errors.Wrapf(err, "failed to make sync fifo")
	}

	spec.Mounts = append(spec.Mounts, specs.Mount{
		Source:      clxc.runtimePath(internal.ConfigDir),
		Destination: strings.Trim(internal.ConfigDir, "/"),
		Type:        "bind",
		Options:     []string{"bind", "ro", "nodev", "nosuid"},
	})

	// pass context information as environment variables to hook scripts
	if err := clxc.setConfigItem("lxc.hook.version", "1"); err != nil {
		return err
	}
	if err := clxc.setConfigItem("lxc.hook.mount", clxc.HookCommand); err != nil {
		return err
	}

	path := "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	for _, kv := range spec.Process.Env {
		if strings.HasPrefix(kv, "PATH=") {
			path = kv
		}
	}
	if err := clxc.setConfigItem("lxc.environment", path); err != nil {
		return err
	}
	if err := clxc.setConfigItem("lxc.environment", envStateCreated); err != nil {
		return err
	}

	// create destination file for bind mount
	initBin := clxc.runtimePath(internal.InitCmd)
	err = touchFile(initBin, 0)
	if err != nil {
		return errors.Wrapf(err, "failed to create %s", initBin)
	}
	spec.Mounts = append(spec.Mounts, specs.Mount{
		Source:      clxc.InitCommand,
		Destination: internal.InitCmd,
		Type:        "bind",
		Options:     []string{"bind", "ro", "nosuid"},
	})
	return clxc.setConfigItem("lxc.init.cmd", internal.InitCmd)
}

func touchFile(filePath string, perm os.FileMode) error {
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDONLY, perm)
	if err == nil {
		return f.Close()
	}
	return err
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

func makeSyncFifo(fifoFilename string) error {
	prevMask := unix.Umask(0000)
	defer unix.Umask(prevMask)
	if err := unix.Mkfifo(fifoFilename, 0666); err != nil {
		return errors.Wrapf(err, "failed to make fifo '%s'", fifoFilename)
	}
	return nil
}

func configureContainerSecurity(c *lxc.Container, spec *specs.Spec) error {
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

func startContainer(spec *specs.Spec, timeout time.Duration) error {
	configFilePath := clxc.runtimePath("config")
	cmd := exec.Command(clxc.StartCommand, clxc.Container.Name(), clxc.RuntimeRoot, configFilePath)
	// Start container with a clean environment.
	// LXC will export variables defined in the config lxc.environment.
	// The environment variables defined by the container spec are exported within the init cmd CRIO_LXC_INIT_CMD.
	// This is required because environment variables defined by containers contain newlines and other tokens
	// that can not be handled properly by lxc.
	cmd.Env = []string{}

	if clxc.ConsoleSocket != "" {
		if err := saveConfig(configFilePath); err != nil {
			return err
		}
		return startConsole(cmd, clxc.ConsoleSocket)
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

	if err := saveConfig(configFilePath); err != nil {
		return err
	}

	if err := internal.WriteSpec(spec, clxc.runtimePath(internal.InitSpec)); err != nil {
		return errors.Wrapf(err, "failed to write init spec")
	}

	err := cmd.Start()
	if err != nil {
		return err
	}

	if clxc.PidFile != "" {
		log.Debug().Str("path:", clxc.PidFile).Msg("creating PID file")
		err := createPidFile(clxc.PidFile, cmd.Process.Pid)
		if err != nil {
			return err
		}
	}

	log.Debug().Msg("waiting for container creation")
	return clxc.waitContainerCreated(timeout)
}

func saveConfig(configFilePath string) error {
	// Write out final config file for debugging and use with lxc-attach:
	// Do not edit config after this.
	err := clxc.Container.SaveConfigFile(configFilePath)
	log.Debug().Err(err).Str("config", configFilePath).Msg("save config file")
	if err != nil {
		return errors.Wrapf(err, "failed to save config file to '%s'", configFilePath)
	}
	return nil
}

func startConsole(cmd *exec.Cmd, consoleSocket string) error {
	addr, err := net.ResolveUnixAddr("unix", consoleSocket)
	if err != nil {
		return errors.Wrap(err, "failed to resolve console socket")
	}
	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		return errors.Wrap(err, "connecting to console socket failed")
	}
	defer conn.Close()
	deadline := time.Now().Add(time.Second * 10)
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
	// man sendmsg 2', 'man unix 3', 'man cmsg 1'
	// see https://blog.cloudflare.com/know-your-scm_rights/
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

		f, err := os.Open(ns.Path)
		if err != nil {
			return errors.Wrapf(err, "failed to open uts namespace %s", ns.Path)
		}
		defer f.Close()

		// setns only affects the current thread
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		err = unix.Setns(int(f.Fd()), unix.CLONE_NEWUTS)
		if err != nil {
			return err
		}
		return unix.Sethostname([]byte(spec.Hostname))
	}
	return nil
}
