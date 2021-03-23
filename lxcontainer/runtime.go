package lxcontainer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

var ErrNotExist = fmt.Errorf("container does not exist")
var ErrExist = fmt.Errorf("container already exists")

type Runtime struct {
	Log

	Root string
	// Use systemd encoded cgroup path (from crio-o/conmon)
	// is true if /etc/crio/crio.conf#cgroup_manager = "systemd"
	SystemdCgroup bool
	// Path for lxc monitor cgroup (lxc specific feature)
	// similar to /etc/crio/crio.conf#conmon_cgroup
	MonitorCgroup string

	// Executables contains names for all (external) executed commands.
	// The excutable name is used as path if it contains a slash, otherwise
	// the PATH environment variable is consulted to resolve the executable path.
	Executables struct {
		Start string
		Init  string
		Hook  string
	}

	// Timeouts for API commands
	Timeouts struct {
		Create time.Duration
		Start  time.Duration
		Kill   time.Duration
		Delete time.Duration
	}

	// feature gates
	Features struct {
		Seccomp       bool
		Capabilities  bool
		Apparmor      bool
		CgroupDevices bool
	}

	// runtime hooks (not OCI runtime hooks)

	Hooks struct {
		// AfterCreateContainer is called right after creating the container runtime directory and descriptor,
		// and before creating the lxc 'config' file for the container.
		AfterCreate func(ctx context.Context, c *Container) error `json:"-"`
	}
}

// Release releases/closes allocated resources (lxc.Container, LogFile)
func (rt *Runtime) Close() error {
	return rt.Log.File.Close()
}

func (rt *Runtime) Start(ctx context.Context, c *Container) error {
	ctx, cancel := context.WithTimeout(ctx, rt.Timeouts.Start)
	defer cancel()

	c.Log.Info().Msg("notify init to start container process")

	state, err := c.State()
	if err != nil {
		return errorf("failed to get container state: %w", err)
	}
	if state.Status != specs.StateCreated {
		return fmt.Errorf("invalid container state. expected %q, but was %q", specs.StateCreated, state.Status)
	}

	return c.Start(ctx)
}

func (rt *Runtime) runStartCmd(ctx context.Context, c *Container) (err error) {
	// #nosec
	cmd := exec.Command(rt.Executables.Start, c.linuxcontainer.Name(), rt.Root, c.ConfigFilePath())
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

	if err := c.SaveConfig(); err != nil {
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
	return c.Kill(ctx, signum)
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
		if err := c.Kill(ctx, unix.SIGKILL); err != nil {
			return errorf("failed to kill container: %w", err)
		}
	}
	if err := c.Destroy(); err != nil {
		return errorf("failed to destroy container: %w", err)
	}
	return nil
}

func (rt *Runtime) Release() error {
	return rt.Log.Close()
}

func ReadSpecProcessJSON(src string) (*specs.Process, error) {
	proc := new(specs.Process)
	err := decodeFileJSON(proc, src)
	return proc, err
}
