package lxcri

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/creack/pty"
	"github.com/drachenfels-de/lxcri/pkg/specki"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"
)

const (
	// BundleConfigFile is the name of the OCI container bundle config file.
	// The content is the JSON encoded specs.Spec.
	BundleConfigFile = "config.json"
)

// Required runtime executables loaded from Runtime.LibexecDir
var (
	// ExecStart starts the liblxc monitor process, similar to lxc-start
	ExecStart = "lxcri-start"
	// ExecHook is run as liblxc hook and creates additional devices and remounts masked paths.
	ExecHook = "lxcri-hook"
	// ExecInit is the container init process that execs the container process.
	ExecInit = "lxcri-init"
)

var (
	// ErrNotExist is returned if the container (runtime dir) does not exist.
	ErrNotExist = fmt.Errorf("container does not exist")
)

// RuntimeFeatures are (security) features supported by the Runtime.
// The supported features are enabled on any Container instance
// created by Runtime.Create.
type RuntimeFeatures struct {
	Seccomp       bool
	Capabilities  bool
	Apparmor      bool
	CgroupDevices bool
}

// HookFunc defines the function type that must be implemented
// by runtime and container (non-OCI) callback hooks.
type HookFunc func(ctx context.Context, c *Container) error

// Hooks are callback functions executed within the container lifecycle.
type Hooks struct {
	// OnCreate is called right after creation of container runtime directory
	// and descriptor, but before the liblxc 'config' file is written.
	// At this point it's possible to add files to the container runtime directory
	// and modify the ContainerConfig accordingly.
	OnCreate HookFunc
}

// Runtime is a factory for creating and managing containers.
// The exported methods of Runtime  are required to implement the
// OCI container runtime interface spec (CRI).
// It shares the common settings
type Runtime struct {
	// Log is the logger used by the runtime.
	Log zerolog.Logger `json:"-"`
	// Root is the file path to the runtime directory.
	// Directories for containers created by the runtime
	// are created within this directory.
	Root string
	// rootfsMount is the directory where liblxc recursively binds
	// the container rootfs before pivoting.
	rootfsMount string
	// Use systemd encoded cgroup path (from crio-o/conmon)
	// is true if /etc/crio/crio.conf#cgroup_manager = "systemd"
	SystemdCgroup bool
	// Path for lxc monitor cgroup (lxc specific feature).
	// This is the cgroup where the liblxc monitor process (lxcri-start)
	// will be placed in. It's similar to /etc/crio/crio.conf#conmon_cgroup
	MonitorCgroup string
	// LibexecDir is the the directory that contains the runtime executables.
	LibexecDir string
	// Featuress are runtime (security) features that apply to all containers
	// created by the runtime.
	Features RuntimeFeatures
	// Hooks contains all callback functions supported by the runtime.
	// These hooks are different from the hooks that are
	// defined within the OCI runtime spec.
	Hooks `json:"-"`

	// Environment passed to `lxcri-start`
	env []string

	// privileged is set by Runtime.Init if user has root privileges.
	privileged bool
}

func (rt *Runtime) libexec(name string) string {
	return filepath.Join(rt.LibexecDir, name)
}

// Init initializes the runtime instance.
// It creates required directories and checks the hosts system configuration.
// Unsupported runtime features are disabled and a warning message is logged.
// Init must be called once for a runtime instance before calling any other method.
func (rt *Runtime) Init() error {
	rt.rootfsMount = filepath.Join(rt.Root, ".rootfs")
	if err := os.MkdirAll(rt.rootfsMount, 0777); err != nil {
		return errorf("failed to create directory for rootfs mount %s: %w", rt.rootfsMount, err)
	}
	if err := os.Chmod(rt.rootfsMount, 0777); err != nil {
		return errorf("failed to 'chmod 0777 %s': %w", err)
	}

	rt.privileged = os.Getuid() == 0

	rt.keepEnv("HOME", "XDG_RUNTIME_DIR", "PATH")

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

func (rt *Runtime) checkConfig(cfg *ContainerConfig) error {
	if len(cfg.ContainerID) == 0 {
		return errorf("missing container ID")
	}
	return rt.checkSpec(cfg.Spec)
}

func (rt *Runtime) checkSpec(spec *specs.Spec) error {
	if spec.Root == nil {
		return errorf("spec.Root is nil")
	}
	if len(spec.Root.Path) == 0 {
		return errorf("empty spec.Root.Path")
	}

	if spec.Process == nil {
		return errorf("spec.Process is nil")
	}

	if len(spec.Process.Args) == 0 {
		return errorf("specs.Process.Args is empty")
	}

	if spec.Process.Cwd == "" {
		rt.Log.Info().Msg("specs.Process.Cwd is unset defaulting to '/'")
		spec.Process.Cwd = "/"
	}

	yes, err := isHostNamespaceShared(spec.Linux.Namespaces, specs.MountNamespace)
	if err != nil {
		return err
	}
	if yes {
		return errorf("container wants to share the hosts mount namespace")
	}

	// It should be best practise not to do so, but there are containers that
	// want to share the hosts PID namespaces. e.g sonobuoy/sonobuoy-systemd-logs-daemon-set
	yes, err = isHostNamespaceShared(spec.Linux.Namespaces, specs.PIDNamespace)
	if err != nil {
		return err
	}
	if yes {
		rt.Log.Info().Msg("container wil share the hosts PID namespace")
	}
	return nil
}

func (rt *Runtime) keepEnv(names ...string) {
	for _, n := range names {
		if val := os.Getenv(n); val != "" {
			rt.env = append(rt.env, n+"="+val)
		}
	}
}

// Load loads a container from the runtime directory.
// The container must have been created with Runtime.Create.
func (rt *Runtime) Load(containerID string) (*Container, error) {
	dir := filepath.Join(rt.Root, containerID)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, ErrNotExist
	}
	c := &Container{
		ContainerConfig: &ContainerConfig{},
		runtimeDir:      dir,
	}
	if err := c.load(); err != nil {
		return nil, err
	}
	return c, nil
}

// Start starts the given container.
// Start simply unblocks the container init process `lxcri-init`,
// which then executes the actuall container process.
// The given container must have been created with Runtime.Create.
func (rt *Runtime) Start(ctx context.Context, c *Container) error {
	rt.Log.Info().Msg("notify init to start container process")

	state, err := c.State()
	if err != nil {
		return errorf("failed to get container state: %w", err)
	}
	if state.SpecState.Status != specs.StateCreated {
		return fmt.Errorf("invalid container state. expected %q, but was %q", specs.StateCreated, state.SpecState.Status)
	}

	return c.start(ctx)
}

func (rt *Runtime) runStartCmd(ctx context.Context, c *Container) (err error) {
	// #nosec
	cmd := exec.Command(rt.libexec(ExecStart), c.LinuxContainer.Name(), rt.Root, c.ConfigFilePath())
	cmd.Env = rt.env
	cmd.Dir = c.RuntimePath()

	if c.ConsoleSocket == "" && !c.Spec.Process.Terminal {
		// Inherit stdio from calling process (conmon).
		// lxc.console.path must be set to 'none' or stdio of init process is replaced with a PTY by lxc
		if err := c.SetConfigItem("lxc.console.path", "none"); err != nil {
			return err
		}
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	// NOTE any config change via clxc.SetConfigItem
	// must be done before calling SaveConfigFile
	err = c.LinuxContainer.SaveConfigFile(c.ConfigFilePath())
	if err != nil {
		return errorf("failed to save config file to %q: %w", c.ConfigFilePath(), err)
	}

	rt.Log.Debug().Msg("starting lxc monitor process")
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

	rt.Log.Debug().Msg("waiting for init")
	if err := c.waitCreated(ctx); err != nil {
		return err
	}

	rt.Log.Info().Int("pid", cmd.Process.Pid).Msg("init process is running, container is created")
	c.CreatedAt = time.Now()
	c.Pid = cmd.Process.Pid
	return nil
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

// Kill sends the signal signum to the container init process.
func (rt *Runtime) Kill(ctx context.Context, c *Container, signum unix.Signal) error {
	state, err := c.ContainerState()
	if err != nil {
		return err
	}
	if state == specs.StateStopped {
		return errorf("container already stopped")
	}
	return c.kill(ctx, signum)
}

// Delete removes the container from the runtime directory.
// The container must be stopped or force must be set to true.
// If the container is not stopped but force is set to true,
// the container will be killed with unix.SIGKILL.
func (rt *Runtime) Delete(ctx context.Context, containerID string, force bool) error {
	rt.Log.Info().Bool("force", force).Msg("delete container")
	c, err := rt.Load(containerID)
	if err == ErrNotExist {
		rt.Log.Info().Msg("container does not exist")
		return nil
	}
	if err != nil {
		rt.Log.Warn().Msgf("deleting runtime dir for unloadable container: %s", err)
		return os.RemoveAll(filepath.Join(rt.Root, containerID))
	}
	state, err := c.ContainerState()
	if err != nil {
		return err
	}
	if state != specs.StateStopped {
		if !force {
			return errorf("container is not not stopped (current state %s)", state)
		}
		if err := c.kill(ctx, unix.SIGKILL); err != nil {
			return errorf("failed to kill container: %w", err)
		}
	}

	if c.CgroupDir != "" {
		// In rare cases processes might escape from kill.
		// This could happens if the container does not use pid_namespaces.
		// Since every container get's it's own cgroup, we can
		// try to kill all processes within the containers cgroup tree.
		if err := drainCgroup(ctx, c.CgroupDir, unix.SIGKILL); err != nil {
			rt.Log.Warn().Msgf("draining cgroup failed: %s", err)
		}
		rt.Log.Info().Msg("cgroup drained")
	}

	if err := c.destroy(); err != nil {
		return errorf("failed to destroy container: %w", err)
	}
	return nil
}
