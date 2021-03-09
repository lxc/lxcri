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

	"golang.org/x/sys/unix"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog"
	"gopkg.in/lxc/go-lxc.v2"
)

var ErrNotExist = fmt.Errorf("container does not exist")
var ErrExist = fmt.Errorf("container already exists")

type Runtime struct {
	Container *lxc.Container
	ContainerInfo

	// [ global settings ]
	LogFile           *os.File
	LogFilePath       string
	LogLevel          string
	LogTimestamp      string
	ContainerLogLevel string
	SystemdCgroup     bool
	MonitorCgroup     string

	StartCommand  string
	InitCommand   string
	ContainerHook string

	Log zerolog.Logger
}

// createContainer creates a new container.
// It must only be called once during the lifecycle of a container.
func (c *Runtime) createContainer(spec *specs.Spec) error {
	if c.runtimePathExists() {
		return ErrExist
	}

	if err := os.MkdirAll(c.RuntimePath(), 0700); err != nil {
		return fmt.Errorf("failed to create container dir: %w", err)
	}

	// An empty tmpfile is created to ensure that createContainer can only succeed once.
	// The config file is atomically activated in saveConfig.
	// #nosec
	f, err := os.OpenFile(c.RuntimePath(".config"), os.O_EXCL|os.O_CREATE|os.O_RDWR, 0640)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close empty config tmpfile: %w", err)
	}

	if spec.Linux.CgroupsPath == "" {
		return fmt.Errorf("empty cgroups path in spec")
	}
	if c.SystemdCgroup {
		c.CgroupDir = parseSystemdCgroupPath(spec.Linux.CgroupsPath)
	} else {
		c.CgroupDir = spec.Linux.CgroupsPath
	}

	c.MonitorCgroupDir = filepath.Join(c.MonitorCgroup, c.ContainerID+".scope")

	if err := createCgroup(filepath.Dir(c.CgroupDir), allControllers); err != nil {
		return err
	}

	c.Annotations = spec.Annotations
	c.Namespaces = spec.Linux.Namespaces

	if err := c.ContainerInfo.Create(); err != nil {
		return err
	}

	container, err := lxc.NewContainer(c.ContainerID, c.RuntimeRoot)
	if err != nil {
		return err
	}
	c.Container = container
	return c.setContainerLogLevel()
}

// loadContainer checks for the existence of the lxc config file.
// It returns an error if the config file does not exist.
func (c *Runtime) loadContainer() error {
	if !c.runtimePathExists() {
		return ErrNotExist
	}

	if err := c.ContainerInfo.Load(); err != nil {
		return fmt.Errorf("failed to load container config: %w", err)
	}

	if _, err := os.Stat(c.ConfigFilePath()); err != nil {
		return fmt.Errorf("failed to load lxc config file: %w", err)
	}
	container, err := lxc.NewContainer(c.ContainerID, c.RuntimeRoot)
	if err != nil {
		return fmt.Errorf("failed to create new lxc container: %w", err)
	}
	c.Container = container
	err = container.LoadConfigFile(c.ConfigFilePath())
	if err != nil {
		return fmt.Errorf("failed to load config file: %w", err)
	}

	return c.setContainerLogLevel()
}

func (c *Runtime) configureCgroupPath() error {
	if err := c.setConfigItem("lxc.cgroup.relative", "0"); err != nil {
		return err
	}

	if err := c.setConfigItem("lxc.cgroup.dir", c.CgroupDir); err != nil {
		return err
	}

	if c.supportsConfigItem("lxc.cgroup.dir.monitor.pivot") {
		if err := c.setConfigItem("lxc.cgroup.dir.monitor.pivot", c.MonitorCgroup); err != nil {
			return err
		}
	}

	/*
		// @since lxc @a900cbaf257c6a7ee9aa73b09c6d3397581d38fb
		// checking for on of the config items shuld be enough, because they were introduced together ...
		if supportsConfigItem("lxc.cgroup.dir.container", "lxc.cgroup.dir.monitor") {
			if err := c.setConfigItem("lxc.cgroup.dir.container", c.CgroupDir); err != nil {
				return err
			}
			if err := c.setConfigItem("lxc.cgroup.dir.monitor", c.MonitorCgroupDir); err != nil {
				return err
			}
		} else {
			if err := c.setConfigItem("lxc.cgroup.dir", c.CgroupDir); err != nil {
				return err
			}
		}
		if supportsConfigItem("lxc.cgroup.dir.monitor.pivot") {
			if err := c.setConfigItem("lxc.cgroup.dir.monitor.pivot", c.MonitorCgroup); err != nil {
				return err
			}
		}
	*/
	return nil
}

// Release releases/closes allocated resources (lxc.Container, LogFile)
func (c Runtime) Release() error {
	if c.Container != nil {
		if err := c.Container.Release(); err != nil {
			c.Log.Error().Err(err).Msg("failed to release container")
		}
		c.Container = nil
	}
	if c.LogFile != nil {
		err := c.LogFile.Close()
		c.LogFile = nil
		return err
	}
	return nil
}

func (c *Runtime) ConfigureLogging(cmdName string) error {
	logDir := filepath.Dir(c.LogFilePath)
	err := os.MkdirAll(logDir, 0750)
	if err != nil {
		return fmt.Errorf("failed to create log file directory %s: %w", logDir, err)
	}

	c.LogFile, err = os.OpenFile(c.LogFilePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return err
	}

	zerolog.LevelFieldName = "l"
	zerolog.MessageFieldName = "m"

	// match liblxc timestamp format
	zerolog.TimestampFieldName = "t"
	zerolog.TimeFieldFormat = c.LogTimestamp
	zerolog.TimestampFunc = func() time.Time {
		return time.Now().UTC()
	}

	// TODO only log caller information in debug and trace level
	zerolog.CallerFieldName = "c"
	zerolog.CallerMarshalFunc = func(file string, line int) string {
		return filepath.Base(file) + ":" + strconv.Itoa(line)
	}

	// NOTE Unfortunately it's not possible change the possition of the timestamp.
	// The ttimestamp is appended to the to the log output because it is dynamically rendered
	// see https://github.com/rs/zerolog/issues/109
	c.Log = zerolog.New(c.LogFile).With().Timestamp().Caller().
		Str("cmd", cmdName).Str("cid", c.ContainerID).Logger()

	level, err := zerolog.ParseLevel(strings.ToLower(c.LogLevel))
	if err != nil {
		level = zerolog.InfoLevel
		c.Log.Warn().Err(err).Str("val", c.LogLevel).Stringer("default", level).
			Msg("failed to parse log-level - fallback to default")
	}
	zerolog.SetGlobalLevel(level)
	return nil
}

func (c *Runtime) setContainerLogLevel() error {
	// Never let lxc write to stdout, stdout belongs to the container init process.
	// Explicitly disable it - allthough it is currently the default.
	c.Container.SetVerbosity(lxc.Quiet)
	// The log level for a running container is set, and may change, per runtime call.
	err := c.Container.SetLogLevel(c.parseContainerLogLevel())
	if err != nil {
		return fmt.Errorf("failed to set container loglevel: %w", err)
	}
	if err := c.Container.SetLogFile(c.LogFilePath); err != nil {
		return fmt.Errorf("failed to set container log file: %w", err)
	}
	return nil
}

func (c *Runtime) parseContainerLogLevel() lxc.LogLevel {
	switch strings.ToLower(c.ContainerLogLevel) {
	case "trace":
		return lxc.TRACE
	case "debug":
		return lxc.DEBUG
	case "info":
		return lxc.INFO
	case "notice":
		return lxc.NOTICE
	case "warn":
		return lxc.WARN
	case "error":
		return lxc.ERROR
	case "crit":
		return lxc.CRIT
	case "alert":
		return lxc.ALERT
	case "fatal":
		return lxc.FATAL
	default:
		c.Log.Warn().Str("val", c.ContainerLogLevel).
			Stringer("default", lxc.WARN).
			Msg("failed to parse container-log-level - fallback to default")
		return lxc.WARN
	}
}

func (c *Runtime) isContainerStopped() bool {
	return c.Container.State() == lxc.STOPPED
}

func (c *Runtime) waitCreated(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			state := c.Container.State()
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

func (c *Runtime) waitNot(ctx context.Context, state specs.ContainerState) error {
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

func (c *Runtime) wait(ctx context.Context, state lxc.State) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		default:
			if c.Container.State() == state {
				return true
			}
			time.Sleep(time.Millisecond * 100)
		}
	}
}

func (c *Runtime) getContainerState() (specs.ContainerState, error) {
	state := c.Container.State()
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
func (c *Runtime) getContainerInitState() (specs.ContainerState, error) {
	initPid := c.Container.InitPid()
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

func (c *Runtime) killContainer(ctx context.Context, signum unix.Signal) error {
	c.Log.Info().Int("signum", int(signum)).Msg("killing container process")
	if signum == unix.SIGKILL || signum == unix.SIGTERM {
		if err := c.setConfigItem("lxc.signal.stop", strconv.Itoa(int(signum))); err != nil {
			return err
		}
		if err := c.Container.Stop(); err != nil {
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

// "Note that resources associated with the container,
// but not created by this container, MUST NOT be deleted."
// TODO - because we set rootfs.managed=0, Destroy() doesn't
// delete the /var/lib/lxc/$containerID/config file:
func (c *Runtime) destroy() error {
	if c.Container != nil {
		if err := c.Container.Destroy(); err != nil {
			return fmt.Errorf("failed to destroy container: %w", err)
		}
	}

	if c.ContainerInfo.CgroupDir != "" {
		err := deleteCgroup(c.CgroupDir)
		if err != nil && !os.IsNotExist(err) {
			c.Log.Warn().Err(err).Str("file", c.CgroupDir).Msg("failed to remove cgroup dir")
		}
	}

	return os.RemoveAll(c.RuntimePath())
}

// saveConfig creates and atomically enables the lxc config file.
// It must be called after #createContainer and only once.
// Any config changes via clxc.setConfigItem must be done
// before calling saveConfig.
func (c *Runtime) saveConfig() error {
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
	err := c.Container.SaveConfigFile(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to save config file to %q: %w", tmpFile, err)
	}
	if err := os.Rename(tmpFile, cfgFile); err != nil {
		return fmt.Errorf("failed to rename config file: %w", err)
	}
	return nil
}

func (c *Runtime) Start(ctx context.Context) error {
	c.Log.Info().Msg("notify init to start container process")

	err := c.loadContainer()
	if err != nil {
		return errorf("failed to load container: %w", err)
	}

	state, err := c.getContainerState()
	if err != nil {
		return errorf("failed to get container state: %w", err)
	}
	if state != specs.StateCreated {
		return fmt.Errorf("invalid container state. expected %q, but was %q", specs.StateCreated, state)
	}

	done := make(chan error)
	go func() {
		done <- c.readFifo()
	}()

	select {
	case <-ctx.Done():
		return errorf("syncfifo timeout: %w", ctx.Err())
	case err := <-done:
		if err != nil {
			return errorf("failed to read from syncfifo: %w", err)
		}
	}
	// wait for container state to change
	return c.waitNot(ctx, specs.StateCreated)
}

func (c *Runtime) syncFifoPath() string {
	return c.RuntimePath(initDir, "syncfifo")
}

func (c *Runtime) readFifo() error {
	// #nosec
	f, err := os.OpenFile(c.syncFifoPath(), os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	// can not set deadline on fifo
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

func (c *Runtime) Delete(ctx context.Context, force bool) error {
	err := c.loadContainer()
	if err == ErrNotExist {
		c.Log.Info().Msg("container does not exist")
		return nil
	}
	c.Log.Info().Bool("force", force).Msg("delete container")
	if err == nil && !c.isContainerStopped() {
		if !force {
			return errorf("container is not not stopped (current state %s)", c.Container.State())
		}
		if err := c.killContainer(ctx, unix.SIGKILL); err != nil {
			return errorf("failed to kill container: %w", err)
		}
	}
	if err := c.destroy(); err != nil {
		return errorf("failed to destroy container: %w", err)
	}
	return nil
}

func (c *Runtime) State() (*specs.State, error) {
	err := c.loadContainer()
	if err != nil {
		return nil, errorf("failed to load container: %w", err)
	}

	pid, err := c.Pid()
	if err != nil {
		return nil, errorf("failed to load pidfile: %w", err)
	}

	state := &specs.State{
		Version:     specs.Version,
		ID:          c.Container.Name(),
		Bundle:      c.BundlePath,
		Pid:         pid,
		Annotations: c.Annotations,
	}

	state.Status, err = c.getContainerState()
	if err != nil {
		return nil, errorf("failed to get container state: %w", err)
	}

	c.Log.Info().Int("pid", state.Pid).Str("status", string(state.Status)).Msg("container state")
	return state, nil
}

func (c *Runtime) Kill(ctx context.Context, signum unix.Signal) error {
	err := c.loadContainer()
	if err != nil {
		return errorf("failed to load container: %w", err)
	}
	state := c.Container.State()
	if state != lxc.RUNNING {
		return errorf("can only kill container in state lxc.RUNNING but was %q", state)
	}
	if err := c.killContainer(ctx, signum); err != nil {
		return errorf("failed to kill container: %w", err)
	}
	return nil
}

func (c *Runtime) ExecDetached(args []string, proc *specs.Process) (pid int, err error) {
	err = c.loadContainer()
	if err != nil {
		return 0, errorf("failed to load container: %w", err)
	}

	opts, err := attachOptions(proc, c.Namespaces)
	if err != nil {
		return 0, errorf("failed to create attach options: %w", err)
	}

	c.Log.Info().Strs("args", args).
		Int("uid", opts.UID).Int("gid", opts.GID).
		Ints("groups", opts.Groups).Msg("execute cmd")

	pid, err = c.Container.RunCommandNoWait(args, opts)
	if err != nil {
		return pid, errorf("failed to run exec cmd detached: %w", err)
	}
	return pid, nil
}

func (c *Runtime) Exec(args []string, proc *specs.Process) (exitStatus int, err error) {
	err = c.loadContainer()
	if err != nil {
		return 0, errorf("failed to load container: %w", err)
	}
	opts, err := attachOptions(proc, c.Namespaces)
	if err != nil {
		return 0, errorf("failed to create attach options: %w", err)
	}
	exitStatus, err = c.Container.RunCommandStatus(args, opts)
	if err != nil {
		return exitStatus, errorf("failed to run exec cmd: %w", err)
	}
	return exitStatus, nil
}

func (c *Runtime) getConfigItem(key string) string {
	vals := c.Container.ConfigItem(key)
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

func (c *Runtime) setConfigItem(key, value string) error {
	err := c.Container.SetConfigItem(key, value)
	if err != nil {
		return fmt.Errorf("failed to set config item '%s=%s': %w", key, value, err)
	}
	c.Log.Debug().Str("lxc.config", key).Str("val", value).Msg("set config item")
	return nil
}

func (c *Runtime) supportsConfigItem(keys ...string) bool {
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
