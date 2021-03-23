package lxcontainer

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"
	"gopkg.in/lxc/go-lxc.v2"
)

type ContainerConfig struct {
	*specs.Spec

	SpecPath string

	RuntimeDir string

	ContainerID string
	CreatedAt   time.Time

	BundlePath    string
	ConsoleSocket string `json:",omitempty"`

	// PidFile is the absolute path to the PID file of the container monitor process (crio-lxc-start)
	PidFile          string
	MonitorCgroupDir string

	CgroupDir string

	// lxc log file and level
	LogLevel string
	LogFile  string

	Log zerolog.Logger `json:"-"`
}

func (cfg ContainerConfig) ConfigFilePath() string {
	return cfg.RuntimePath("config")
}

func (cfg ContainerConfig) syncFifoPath() string {
	return cfg.RuntimePath(initDir, "syncfifo")
}

// RuntimePath returns the absolute path witin the container root
func (cfg ContainerConfig) RuntimePath(subPath ...string) string {
	return filepath.Join(cfg.RuntimeDir, filepath.Join(subPath...))
}

func (cfg ContainerConfig) runtimeDirExists() bool {
	if _, err := os.Stat(cfg.RuntimeDir); err == nil {
		return true
	}
	return false
}

func (cfg ContainerConfig) Pid() (int, error) {
	// #nosec
	data, err := ioutil.ReadFile(cfg.PidFile)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(data))
	return strconv.Atoi(s)
}

func (c *ContainerConfig) LoadSpecJson(p string) error {
	c.SpecPath = p
	c.Spec = &specs.Spec{}
	return decodeFileJSON(c.Spec, p)
}

// ContainerInfo holds the information about a single container.
// It is created at 'create' within the container runtime dir and not changed afterwards.
// It is removed when the container is deleted.
type Container struct {
	linuxcontainer *lxc.Container `json:"-"`
	*ContainerConfig
}

func (c *Container) create() error {
	if c.runtimeDirExists() {
		return ErrExist
	}
	if err := os.MkdirAll(c.RuntimeDir, 0700); err != nil {
		return fmt.Errorf("failed to create container dir: %w", err)
	}

	// An empty tmpfile is created to ensure that createContainer can only succeed once.
	// The config file is atomically activated in SaveConfig.
	// #nosec
	f, err := os.OpenFile(c.RuntimePath(".config"), os.O_EXCL|os.O_CREATE|os.O_RDWR, 0640)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close empty config tmpfile: %w", err)
	}

	c.linuxcontainer, err = lxc.NewContainer(c.ContainerID, filepath.Dir(c.RuntimeDir))
	if err != nil {
		return err
	}

	return nil
}

func (c *Container) load() error {
	if !c.runtimeDirExists() {
		return ErrNotExist
	}

	err := decodeFileJSON(&c.ContainerConfig, c.RuntimePath("container.json"))
	if err != nil {
		return fmt.Errorf("failed to load container config: %w", err)
	}

	_, err = os.Stat(c.ConfigFilePath())
	if err != nil {
		return fmt.Errorf("failed to load lxc config file: %w", err)
	}
	c.linuxcontainer, err = lxc.NewContainer(c.ContainerID, filepath.Dir(c.RuntimeDir))
	if err != nil {
		return fmt.Errorf("failed to create lxc container: %w", err)
	}

	err = c.linuxcontainer.LoadConfigFile(c.ConfigFilePath())
	if err != nil {
		return fmt.Errorf("failed to load config file: %w", err)
	}
	return nil
}

func (c *Container) waitCreated(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			state := c.linuxcontainer.State()
			if !(state == lxc.RUNNING) {
				c.Log.Debug().Stringer("state", state).Msg("wait for state lxc.RUNNING")
				time.Sleep(time.Millisecond * 100)
				continue
			}
			initState, err := c.getContainerInitState()
			if err != nil {
				return err
			}
			if initState == specs.StateCreated {
				return nil
			}
			return fmt.Errorf("unexpected init state %q", initState)
		}
	}
}

func (c *Container) waitNot(ctx context.Context, state specs.ContainerState) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			initState, _ := c.getContainerInitState()
			if initState != state {
				return nil
			}
			time.Sleep(time.Millisecond * 10)
		}
	}
}

func (c *Container) wait(ctx context.Context, state lxc.State) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		default:
			if c.linuxcontainer.State() == state {
				return true
			}
			time.Sleep(time.Millisecond * 100)
		}
	}
}

func (c *Container) State() (*specs.State, error) {
	pid, err := c.Pid()
	if err != nil {
		return nil, errorf("failed to load pidfile: %w", err)
	}

	status, err := c.ContainerState()
	if err != nil {
		return nil, errorf("failed go get container status: %w", err)
	}

	state := &specs.State{
		Version:     specs.Version,
		ID:          c.ContainerID,
		Bundle:      c.BundlePath,
		Pid:         pid,
		Annotations: c.Annotations,
		Status:      status,
	}
	return state, nil
}

func (c *Container) ContainerState() (specs.ContainerState, error) {
	state := c.linuxcontainer.State()
	switch state {
	case lxc.STOPPED:
		return specs.StateStopped, nil
	case lxc.STARTING:
		return specs.StateCreating, nil
	case lxc.RUNNING, lxc.STOPPING, lxc.ABORTING, lxc.FREEZING, lxc.FROZEN, lxc.THAWED:
		return c.getContainerInitState()
	default:
		return specs.StateStopped, fmt.Errorf("unsupported lxc container state %q", state)
	}
}

// getContainerInitState returns the detailed state of the container init process.
// This should be called if the container is in state lxc.RUNNING.
// On error the caller should call getContainerState() again
func (c *Container) getContainerInitState() (specs.ContainerState, error) {
	initPid := c.linuxcontainer.InitPid()
	if initPid < 1 {
		return specs.StateStopped, nil
	}
	cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", initPid)
	cmdline, err := ioutil.ReadFile(cmdlinePath)
	if os.IsNotExist(err) {
		// init process died or returned
		return specs.StateStopped, nil
	}
	if err != nil {
		// it's a serious error if cmdlinePath exists but can't be read
		return specs.StateStopped, err
	}

	initCmdline := fmt.Sprintf("/.crio-lxc/init\000%s\000", c.ContainerID)
	if string(cmdline) == initCmdline {
		return specs.StateCreated, nil
	}
	return specs.StateRunning, nil
}

func (c *Container) kill(ctx context.Context, signum unix.Signal) error {
	c.Log.Info().Int("signum", int(signum)).Msg("killing container process")
	if signum == unix.SIGKILL || signum == unix.SIGTERM {
		if err := c.SetConfigItem("lxc.signal.stop", strconv.Itoa(int(signum))); err != nil {
			return err
		}
		if err := c.linuxcontainer.Stop(); err != nil {
			return err
		}

		if !c.wait(ctx, lxc.STOPPED) {
			c.Log.Warn().Msg("failed to stop lxc container")
		}

		// draining the cgroup is required to catch processes that escaped from
		// 'kill' e.g a bash for loop that spawns a new child immediately.
		start := time.Now()
		err := drainCgroup(ctx, c.CgroupDir, signum)
		if err != nil && !os.IsNotExist(err) {
			c.Log.Warn().Err(err).Str("file", c.CgroupDir).Msg("failed to drain cgroup")
		} else {
			c.Log.Info().Dur("duration", time.Since(start)).Str("file", c.CgroupDir).Msg("cgroup drained")
		}
		return err
	}

	//  send non-terminating signals to monitor process
	pid, err := c.Pid()
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to load pidfile: %w", err)
	}
	if pid > 1 {
		c.Log.Info().Int("pid", pid).Int("signal", int(signum)).Msg("sending signal")
		if err := unix.Kill(pid, 0); err == nil {
			err := unix.Kill(pid, signum)
			if err != unix.ESRCH {
				return fmt.Errorf("failed to send signal %d to container process %d: %w", signum, pid, err)
			}
		}
	}
	return nil
}

// SaveConfig creates and atomically enables the lxc config file.
// It must be called only once. It is automatically called by Runtime#Create.
// Any config changes via clxc.setConfigItem must be done before calling SaveConfig.
// FIXME revise the config file mechanism
func (c *Container) saveConfig() error {
	// createContainer creates the tmpfile
	tmpFile := c.RuntimePath(".config")
	if _, err := os.Stat(tmpFile); err != nil {
		return fmt.Errorf("failed to stat config tmpfile: %w", err)
	}
	// Don't overwrite an existing config.
	cfgFile := c.ConfigFilePath()
	if _, err := os.Stat(cfgFile); err == nil {
		return fmt.Errorf("config file %s already exists", cfgFile)
	}
	err := c.linuxcontainer.SaveConfigFile(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to save config file to %q: %w", tmpFile, err)
	}
	if err := os.Rename(tmpFile, cfgFile); err != nil {
		return fmt.Errorf("failed to rename config file: %w", err)
	}

	p := c.RuntimePath("container.json")
	c.CreatedAt = time.Now()
	return encodeFileJSON(p, c, os.O_EXCL|os.O_CREATE|os.O_RDWR, 0640)
}

func (c *Container) GetConfigItem(key string) string {
	vals := c.linuxcontainer.ConfigItem(key)
	if len(vals) > 0 {
		first := vals[0]
		// some lxc config values are set to '(null)' if unset
		// eg. lxc.cgroup.dir
		if first != "(null)" {
			return first
		}
	}
	return ""
}

func (c *Container) SetConfigItem(key, value string) error {
	err := c.linuxcontainer.SetConfigItem(key, value)
	if err != nil {
		return fmt.Errorf("failed to set config item '%s=%s': %w", key, value, err)
	}
	c.Log.Debug().Str("lxc.config", key).Str("val", value).Msg("set config item")
	return nil
}

func (c *Container) SupportsConfigItem(keys ...string) bool {
	canCheck := lxc.VersionAtLeast(4, 0, 6)
	if !canCheck {
		c.Log.Warn().Msg("lxc.IsSupportedConfigItem is broken in liblxc < 4.0.6")
	}
	for _, key := range keys {
		if canCheck && lxc.IsSupportedConfigItem(key) {
			continue
		}
		c.Log.Info().Str("lxc.config", key).Msg("unsupported config item")
		return false
	}
	return true
}

func (c *Container) Release() error {
	return c.linuxcontainer.Release()
}

// "Note that resources associated with the container,
// but not created by this container, MUST NOT be deleted."
// TODO - because we set rootfs.managed=0, Destroy() doesn't
// delete the /var/lib/lxc/$containerID/config file:
func (c *Container) destroy() error {
	if err := c.linuxcontainer.Destroy(); err != nil {
		return fmt.Errorf("failed to destroy container: %w", err)
	}
	if c.CgroupDir != "" {
		err := deleteCgroup(c.CgroupDir)
		if err != nil && !os.IsNotExist(err) {
			c.Log.Warn().Err(err).Str("file", c.CgroupDir).Msg("failed to remove cgroup dir")
		}
	}
	return os.RemoveAll(c.RuntimePath())
}

func (c *Container) start(ctx context.Context) error {
	done := make(chan error)
	go func() {
		// FIXME fifo must be unblocked otherwise
		// this may be a goroutine leak
		done <- c.readFifo()
	}()

	select {
	case <-ctx.Done():
		return errorf("syncfifo timeout: %w", ctx.Err())
		// TODO write to fifo here and fallthrough ?
	case err := <-done:
		if err != nil {
			return errorf("failed to read from syncfifo: %w", err)
		}
	}
	// wait for container state to change
	return c.waitNot(ctx, specs.StateCreated)
}

func (c *Container) readFifo() error {
	// #nosec
	f, err := os.OpenFile(c.syncFifoPath(), os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	// NOTE it's not possible to set an IO deadline on a fifo
	// #nosec
	defer f.Close()

	data := make([]byte, len(c.ContainerID))
	_, err = f.Read(data)
	if err != nil {
		return fmt.Errorf("problem reading from fifo: %w", err)
	}
	if c.ContainerID != string(data) {
		return fmt.Errorf("bad fifo content: %s", string(data))
	}
	return nil
}

func (c *Container) ExecDetached(args []string, proc *specs.Process) (pid int, err error) {
	opts, err := attachOptions(proc, c.Linux.Namespaces)
	if err != nil {
		return 0, errorf("failed to create attach options: %w", err)
	}

	c.Log.Info().Strs("args", args).
		Int("uid", opts.UID).Int("gid", opts.GID).
		Ints("groups", opts.Groups).Msg("execute cmd")

	pid, err = c.linuxcontainer.RunCommandNoWait(args, opts)
	if err != nil {
		return pid, errorf("failed to run exec cmd detached: %w", err)
	}
	return pid, nil
}

func (c *Container) Exec(args []string, proc *specs.Process) (exitStatus int, err error) {
	opts, err := attachOptions(proc, c.Linux.Namespaces)
	if err != nil {
		return 0, errorf("failed to create attach options: %w", err)
	}
	exitStatus, err = c.linuxcontainer.RunCommandStatus(args, opts)
	if err != nil {
		return exitStatus, errorf("failed to run exec cmd: %w", err)
	}
	return exitStatus, nil
}

func attachOptions(procSpec *specs.Process, ns []specs.LinuxNamespace) (lxc.AttachOptions, error) {
	opts := lxc.AttachOptions{
		StdinFd:  0,
		StdoutFd: 1,
		StderrFd: 2,
	}

	clone, err := cloneFlags(ns)
	if err != nil {
		return opts, err
	}
	opts.Namespaces = clone

	if procSpec != nil {
		opts.Cwd = procSpec.Cwd
		// Use the environment defined by the process spec.
		opts.ClearEnv = true
		opts.Env = procSpec.Env

		opts.UID = int(procSpec.User.UID)
		opts.GID = int(procSpec.User.GID)
		if n := len(procSpec.User.AdditionalGids); n > 0 {
			opts.Groups = make([]int, n)
			for i, g := range procSpec.User.AdditionalGids {
				opts.Groups[i] = int(g)
			}
		}
	}
	return opts, nil
}
