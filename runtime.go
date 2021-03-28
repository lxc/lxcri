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
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"

	"github.com/drachenfels-de/lxcri/log"
)

// Required runtime executables loaded from Runtime.LibexecDir
const (
	// ExecStart starts the liblxc monitor process, similar to lxc-start
	ExecStart = "lxcri-start"
	// ExecHook is run as liblxc hook and creates additional devices and remounts masked paths.
	ExecHook = "lxcri-hook"
	// ExecInit is the container init process that execs the container process.
	ExecInit = "lxcri-init"
)

var (
	ErrNotExist = fmt.Errorf("container does not exist")
	ErrExist    = fmt.Errorf("container already exists")
)

// RuntimeTimeouts are timeouts for Runtime API calls.
type RuntimeTimeouts struct {
	Create time.Duration
	Start  time.Duration
	Kill   time.Duration
	Delete time.Duration
}

// RuntimeFeatures are (security) features supported by the Runtime.
// The supported features are enabled on any Container instance
// created by Runtime.Create.
type RuntimeFeatures struct {
	Seccomp       bool
	Capabilities  bool
	Apparmor      bool
	CgroupDevices bool
}

type RuntimeHook func(ctx context.Context, c *Container) error

// RuntimeHooks are callback functions executed within the container lifecycle.
type Hooks struct {
	// OnCreate is called right after creation of container runtime directory
	// and descriptor, but before the liblxc 'config' file is written.
	// At this point it's possible to add files to the container runtime directory
	// and modify the ContainerConfig.
	OnCreate RuntimeHook
}

type Runtime struct {
	// Log is the logger used by the runtime.
	Log zerolog.Logger
	// Root is the file path to the runtime directory.
	// Directories for containers created by the runtime
	// are created within this directory.
	Root string
	// Use systemd encoded cgroup path (from crio-o/conmon)
	// is true if /etc/crio/crio.conf#cgroup_manager = "systemd"
	SystemdCgroup bool
	// Path for lxc monitor cgroup (lxc specific feature)
	// similar to /etc/crio/crio.conf#conmon_cgroup
	MonitorCgroup string
	// LibexecDir is the the directory that contains the runtime executables.
	LibexecDir string
	// Timeouts are the runtime API command timeouts.
	Timeouts RuntimeTimeouts
	//
	Features RuntimeFeatures

	Hooks `json:"-"`
}

var DefaultRuntime = &Runtime{
	Log:           log.ConsoleLogger(true),
	Root:          "/var/run/lxcri",
	SystemdCgroup: true,
	LibexecDir:    "/usr/libexec/lxcri",

	Timeouts: RuntimeTimeouts{
		Create: time.Second * 60,
		Start:  time.Second * 30,
		Kill:   time.Second * 30,
		Delete: time.Second * 60,
	},
	Features: RuntimeFeatures{
		Seccomp:       true,
		Capabilities:  true,
		Apparmor:      true,
		CgroupDevices: true,
	},
}

func (rt *Runtime) libexec(name string) string {
	return filepath.Join(rt.LibexecDir, name)
}

// CheckSystem is a wrapper around DefaultRuntime.CheckSystem
func CheckSystem() error {
	return DefaultRuntime.CheckSystem()
}

// Create is a wrapper around DefaultRuntime.Create
func Create(ctx context.Context, cfg *ContainerConfig) (*Container, error) {
	return DefaultRuntime.Create(ctx, cfg)
}

// Load is a wrapper around DefaultRuntime.Load
func Load(cfg *ContainerConfig) (*Container, error) {
	return DefaultRuntime.Load(cfg)
}

// Start is a wrapper around DefaultRuntime.Start
func Start(ctx context.Context, c *Container) error {
	return DefaultRuntime.Start(ctx, c)
}

// Kill is a wrapper around DefaultRuntime.Kill
func Kill(ctx context.Context, c *Container, signum unix.Signal) error {
	return DefaultRuntime.Kill(ctx, c, signum)
}

// Delete is a wrapper around DefaultRuntime.Delete
func Delete(ctx context.Context, c *Container, force bool) error {
	return DefaultRuntime.Delete(ctx, c, force)
}

func (rt *Runtime) Load(cfg *ContainerConfig) (*Container, error) {
	c := &Container{ContainerConfig: cfg}
	c.RuntimeDir = filepath.Join(rt.Root, c.ContainerID)

	if err := c.load(); err != nil {
		return nil, err
	}
	return c, nil
}

func (rt *Runtime) Start(ctx context.Context, c *Container) error {
	ctx, cancel := context.WithTimeout(ctx, rt.Timeouts.Start)
	defer cancel()

	rt.Log.Info().Msg("notify init to start container process")

	state, err := c.State()
	if err != nil {
		return errorf("failed to get container state: %w", err)
	}
	if state.Status != specs.StateCreated {
		return fmt.Errorf("invalid container state. expected %q, but was %q", specs.StateCreated, state.Status)
	}

	return c.start(ctx)
}

func (rt *Runtime) runStartCmd(ctx context.Context, c *Container) (err error) {
	// #nosec
	cmd := exec.Command(rt.libexec(ExecStart), c.LinuxContainer.Name(), rt.Root, c.ConfigFilePath())
	cmd.Env = []string{}
	cmd.Dir = c.RuntimePath()

	if c.ConsoleSocket == "" && !c.Process.Terminal {
		// Inherit stdio from calling process (conmon).
		// lxc.console.path must be set to 'none' or stdio of init process is replaced with a PTY by lxc
		if err := c.SetConfigItem("lxc.console.path", "none"); err != nil {
			return err
		}
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := c.saveConfig(); err != nil {
		return err
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

	/*
		go func() {
			// NOTE this goroutine may leak until lxcri-start is terminated
			ps, err := cmd.Process.Wait()
			if err != nil {
				rt.Log.Error().Err(err).Msg("failed to wait for start process")
			} else {
				rt.Log.Warn().Int("pid", cmd.Process.Pid).Stringer("status", ps).Msg("start process terminated")
			}
			cancel()
		}()
	*/

	rt.Log.Debug().Msg("waiting for init")
	if err := c.waitCreated(ctx); err != nil {
		return err
	}

	rt.Log.Info().Int("pid", cmd.Process.Pid).Msg("init process is running, container is created")
	// FIXME set PID to container config ?
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

func (rt *Runtime) Kill(ctx context.Context, c *Container, signum unix.Signal) error {
	ctx, cancel := context.WithTimeout(ctx, rt.Timeouts.Kill)
	defer cancel()

	state, err := c.ContainerState()
	if err != nil {
		return err
	}
	if state == specs.StateStopped {
		return errorf("container already stopped")
	}
	return c.kill(ctx, signum)
}

func (rt *Runtime) Delete(ctx context.Context, c *Container, force bool) error {
	ctx, cancel := context.WithTimeout(ctx, rt.Timeouts.Delete)
	defer cancel()

	rt.Log.Info().Bool("force", force).Msg("delete container")
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
	if err := c.destroy(); err != nil {
		return errorf("failed to destroy container: %w", err)
	}
	return nil
}

func ReadSpecProcessJSON(src string) (*specs.Process, error) {
	proc := new(specs.Process)
	err := decodeFileJSON(proc, src)
	return proc, err
}
