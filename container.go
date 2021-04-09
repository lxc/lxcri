package lxcri

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"
	"gopkg.in/lxc/go-lxc.v2"
)

// ContainerConfig is the configuration for a single Container instance.
type ContainerConfig struct {
	// The modified/updated Spec used to generate the liblxc config.
	// It's serialized to a separate file to allow external tool access,
	// without the need to import lxcri.
	*specs.Spec `json:"-"`

	// ContainerID is the identifier of the container.
	// The ContainerID is used as name for the containers runtime directory.
	// The ContainerID must be unique at least through all containers of a runtime.
	// The ContainerID should match the following pattern `[a-z][a-z0-9-_]+`
	ContainerID string

	BundlePath    string
	ConsoleSocket string `json:",omitempty"`

	// PidFile is the absolute PID file path
	// for the container monitor process (ExecStart)
	MonitorCgroupDir string

	CgroupDir string

	// LogFile is the liblxc log file path
	LogFile string
	// LogLevel is the liblxc log level
	LogLevel string

	// Log is the container Logger
	Log zerolog.Logger `json:"-"`

	Hooks `json:"-"`
}

// ConfigFilePath returns the path to the liblxc config file.
func (c Container) ConfigFilePath() string {
	return c.RuntimePath("config")
}

func (c Container) syncFifoPath() string {
	return c.RuntimePath(initDir, "syncfifo")
}

// RuntimePath returns the absolute path to the given sub path
// within the container root.
func (c Container) RuntimePath(subPath ...string) string {
	return filepath.Join(c.runtimeDir, filepath.Join(subPath...))
}

// Container is the runtime state of a container instance.
type Container struct {
	LinuxContainer *lxc.Container `json:"-"`
	*ContainerConfig

	CreatedAt time.Time
	Pid       int

	runtimeDir string
}

// Return true if init user is mapped to runtime user
func (c *Container) isUserUID() bool {
	cuid := uint32(os.Getuid())
	puid := c.Process.User.UID

	// no id mappings
	if len(c.Linux.UIDMappings) == 0 {
		return puid == cuid
	}

	for _, idmap := range c.Linux.UIDMappings {
		if idmap.Size < 1 {
			continue
		}
		maxID := idmap.ContainerID + idmap.Size - 1
		// check if c.Process.UID is contained in the mapping
		if (puid >= idmap.ContainerID) && (puid <= maxID) {
			offset := puid - idmap.ContainerID
			hostid := idmap.HostID + offset
			return hostid == cuid
		}
	}
	return false
}

func (c *Container) create() error {
	if err := os.MkdirAll(c.runtimeDir, 0777); err != nil {
		return fmt.Errorf("failed to create container dir: %w", err)
	}

	if err := os.Chmod(c.runtimeDir, 0777); err != nil {
		return errorf("failed to chmod %s: %w", err)
	}

	f, err := os.OpenFile(c.RuntimePath("config"), os.O_EXCL|os.O_CREATE|os.O_RDWR, 0640)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close empty config tmpfile: %w", err)
	}

	c.LinuxContainer, err = lxc.NewContainer(c.ContainerID, filepath.Dir(c.runtimeDir))
	if err != nil {
		return err
	}

	return nil
}

func (c *Container) load() error {

	err := decodeFileJSON(c, c.RuntimePath("container.json"))
	if err != nil {
		return fmt.Errorf("failed to load container config: %w", err)
	}

	c.Spec = new(specs.Spec)
	err = decodeFileJSON(c.Spec, c.RuntimePath("spec.json"))
	if err != nil {
		return fmt.Errorf("failed to load container spec: %w", err)
	}

	_, err = os.Stat(c.ConfigFilePath())
	if err != nil {
		return fmt.Errorf("failed to load lxc config file: %w", err)
	}
	c.LinuxContainer, err = lxc.NewContainer(c.ContainerID, filepath.Dir(c.runtimeDir))
	if err != nil {
		return fmt.Errorf("failed to create lxc container: %w", err)
	}

	err = c.LinuxContainer.LoadConfigFile(c.ConfigFilePath())
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
			state := c.LinuxContainer.State()
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
			if c.LinuxContainer.State() == state {
				return true
			}
			time.Sleep(time.Millisecond * 100)
		}
	}
}

// State returns the OCI specs.State value for the containers process.
// The State.Pid value is the PID of the liblxc
// container monitor process (lxcri-start).
func (c *Container) State() (*specs.State, error) {
	status, err := c.ContainerState()
	if err != nil {
		return nil, errorf("failed go get container status: %w", err)
	}

	state := &specs.State{
		Version:     specs.Version,
		ID:          c.ContainerID,
		Bundle:      c.BundlePath,
		Pid:         c.Pid,
		Annotations: c.Annotations,
		Status:      status,
	}
	return state, nil
}

// ContainerState returns the current state of the container process,
// as defined by the OCI runtime spec.
func (c *Container) ContainerState() (specs.ContainerState, error) {
	state := c.LinuxContainer.State()
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
	initPid := c.LinuxContainer.InitPid()
	if initPid < 1 {
		return specs.StateStopped, nil
	}
	cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", initPid)
	cmdline, err := os.ReadFile(cmdlinePath)
	if os.IsNotExist(err) {
		// init process died or returned
		return specs.StateStopped, nil
	}
	if err != nil {
		// it's a serious error if cmdlinePath exists but can't be read
		return specs.StateStopped, err
	}

	initCmdline := fmt.Sprintf("/.lxcri/init\000%s\000", c.ContainerID)
	if string(cmdline) == initCmdline {
		return specs.StateCreated, nil
	}
	return specs.StateRunning, nil
}

func (c *Container) kill(ctx context.Context, signum unix.Signal) error {
	c.Log.Info().Int("signum", int(signum)).Msg("killing container process")

	// TODO(race condition) check whether it is save to signal InitPid()
	// To avoid that the PID of the init process is recycled we aquire the pidfd of it.
	// The container init process may have already died here and if the PID is already recycled,
	// the wrong process will be signaled. If the kernel is recent enough.
	// to support pidfd_send_signal (only kernel > 5.6 ?)
	pidfd, err := c.LinuxContainer.InitPidFd()
	if err != nil {
		// since this is best-effort we won't return an error
		c.Log.Warn().Msgf("failed to get init pidfd: %s", err)
	} else {
		defer pidfd.Close()
	}

	// From `man pid_namespaces`: If the "init" process of a PID namespace terminates, the kernel
	// terminates all of the processes in the namespace via a SIGKILL signal.
	// So there is nothing more to do here than to signal the init process.
	// NOTE: The liblxc monitor process `lxcri-start` doesn't propagate all signals to the init process,
	// but handles some signals on its own. E.g SIGHUP tells the monitor process to hang up the terminal
	// and terminate the init process with SIGTERM.
	pid := c.LinuxContainer.InitPid()
	if pid <= 1 {
		return nil
	}

	c.Log.Info().Int("pid", pid).Int("signal", int(signum)).Msg("sending signal")
	err = unix.Kill(pid, signum)
	if err == unix.ESRCH {
		// init already died before sending the signal - not an error
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to send signal %d to container process %d: %w", signum, pid, err)
	}
	return nil
}

// GetConfigItem is a wrapper function and returns the
// first value returned by  *lxc.Container.ConfigItem
func (c *Container) GetConfigItem(key string) string {
	vals := c.LinuxContainer.ConfigItem(key)
	if len(vals) > 0 {
		first := vals[0]
		// some lxc config values are set to '(null)' if unset eg. lxc.cgroup.dir
		// TODO check if this is already fixed
		if first != "(null)" {
			return first
		}
	}
	return ""
}

// SetConfigItem is a wrapper for *lxc.Container.SetConfigItem.
// and only adds additional logging.
func (c *Container) SetConfigItem(key, value string) error {
	err := c.LinuxContainer.SetConfigItem(key, value)
	if err != nil {
		return fmt.Errorf("failed to set config item '%s=%s': %w", key, value, err)
	}
	c.Log.Debug().Str("lxc.config", key).Str("val", value).Msg("set config item")
	return nil
}

// SupportsConfigItem is a wrapper for *lxc.Container.IsSupportedConfig item.
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

// Release releases resources allocated by the container.
func (c *Container) Release() error {
	return c.LinuxContainer.Release()
}

// From OCI runtime spec
// "Note that resources associated with the container, but not
// created by this container, MUST NOT be deleted."
// The *lxc.Container is created with `rootfs.managed=0`,
// so calling *lxc.Container.Destroy will not delete container resources.
func (c *Container) destroy() error {
	if err := c.LinuxContainer.Destroy(); err != nil {
		return fmt.Errorf("failed to destroy container: %w", err)
	}
	if c.CgroupDir != "" {
		err := deleteCgroup(c.CgroupDir)
		if err != nil && !os.IsNotExist(err) {
			return err
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

// ExecDetached executes the given process spec within the container.
// The given process is started and the process PID is returned.
// It's up to the caller to wait for the process to exit using the returned PID.
// The container state must be either specs.StateCreated or specs.StateRunning
func (c *Container) ExecDetached(args []string, proc *specs.Process) (pid int, err error) {
	opts, err := attachOptions(proc, c.Linux.Namespaces)
	if err != nil {
		return 0, errorf("failed to create attach options: %w", err)
	}

	c.Log.Info().Strs("args", args).
		Int("uid", opts.UID).Int("gid", opts.GID).
		Ints("groups", opts.Groups).Msg("execute cmd")

	pid, err = c.LinuxContainer.RunCommandNoWait(args, opts)
	if err != nil {
		return pid, errorf("failed to run exec cmd detached: %w", err)
	}
	return pid, nil
}

// Exec executes the given process spec within the container.
// It waits for the process to exit and returns its exit code.
// The container state must either be specs.StateCreated or specs.StateRunning
func (c *Container) Exec(args []string, proc *specs.Process) (exitStatus int, err error) {
	opts, err := attachOptions(proc, c.Linux.Namespaces)
	if err != nil {
		return 0, errorf("failed to create attach options: %w", err)
	}
	exitStatus, err = c.LinuxContainer.RunCommandStatus(args, opts)
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

func setLog(c *Container) error {
	// Never let lxc write to stdout, stdout belongs to the container init process.
	// Explicitly disable it - allthough it is currently the default.
	c.LinuxContainer.SetVerbosity(lxc.Quiet)
	// The log level for a running container is set, and may change, per runtime call.
	err := c.LinuxContainer.SetLogLevel(parseContainerLogLevel(c.LogLevel))
	if err != nil {
		return fmt.Errorf("failed to set container loglevel: %w", err)
	}
	if err := c.LinuxContainer.SetLogFile(c.LogFile); err != nil {
		return fmt.Errorf("failed to set container log file: %w", err)
	}
	return nil
}

func parseContainerLogLevel(level string) lxc.LogLevel {
	switch strings.ToLower(level) {
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
		return lxc.WARN
	}
}
