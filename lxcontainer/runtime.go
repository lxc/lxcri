package lxcontainer

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog"
	"gopkg.in/lxc/go-lxc.v2"
)

// logging constants
const (
	// liblxc timestamp formattime format
	timeFormatLXCMillis = "20060102150405.000"
)

// ContainerState represents the state of a container.
type ContainerState string

const (
	// StateCreating indicates that the container is being created
	StateCreating ContainerState = "creating"

	// StateCreated indicates that the runtime has finished the create operation
	StateCreated ContainerState = "created"

	// StateRunning indicates that the container process has executed the
	// user-specified program but has not exited
	StateRunning ContainerState = "running"

	// StateStopped indicates that the container process has exited
	StateStopped ContainerState = "stopped"
)

var log zerolog.Logger

var errContainerNotExist = errors.New("container does not exist")
var errContainerExist = errors.New("container already exists")

var version string

func Version() string {
	return fmt.Sprintf("%s (%s) (lxc:%s)", version, runtime.Version(), lxc.Version())
}

type Runtime struct {
	Container *lxc.Container
	ContainerInfo

	Command string

	// [ global settings ]
	LogFile           *os.File
	LogFilePath       string
	LogLevel          string
	ContainerLogLevel string
	SystemdCgroup     bool
	MonitorCgroup     string

	StartCommand         string
	InitCommand          string
	ContainerHook        string
	RuntimeHook          string
	RuntimeHookTimeout   time.Duration
	RuntimeHookRunAlways bool

	// create flags
	ConsoleSocketTimeout time.Duration

	// start flags
	StartTimeout time.Duration
}

// createContainer creates a new container.
// It must only be called once during the lifecycle of a container.
func (c *Runtime) createContainer(spec *specs.Spec) error {
	if _, err := os.Stat(c.ConfigFilePath()); err == nil {
		return errContainerExist
	}

	if err := os.MkdirAll(c.RuntimePath(), 0700); err != nil {
		return errors.Wrap(err, "failed to create container dir")
	}

	// An empty tmpfile is created to ensure that createContainer can only succeed once.
	// The config file is atomically activated in saveConfig.
	// #nosec
	f, err := os.OpenFile(c.RuntimePath(".config"), os.O_EXCL|os.O_CREATE|os.O_RDWR, 0640)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return errors.Wrap(err, "failed to close empty config file")
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

	if err := enableCgroupControllers(filepath.Dir(c.CgroupDir)); err != nil {
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
	if err := c.ContainerInfo.Load(); err != nil {
		return err
	}

	_, err := os.Stat(c.ConfigFilePath())
	if os.IsNotExist(err) {
		return errContainerNotExist
	}
	if err != nil {
		return errors.Wrap(err, "failed to access config file")
	}

	container, err := lxc.NewContainer(c.ContainerID, c.RuntimeRoot)
	if err != nil {
		return errors.Wrap(err, "failed to create new lxc container")
	}
	c.Container = container

	err = container.LoadConfigFile(c.ConfigFilePath())
	if err != nil {
		return errors.Wrap(err, "failed to load config file")
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

	if supportsConfigItem("lxc.cgroup.dir.monitor.pivot") {
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
func (c Runtime) Release(err error) error {
	if c.Container != nil {
		if err := c.Container.Release(); err != nil {
			log.Error().Err(err).Msg("failed to release container")
		}
	}
	if c.LogFile != nil {
		return c.LogFile.Close()
	}
	return nil
}


	cmdDuration := time.Since(startTime)
	if err != nil {
		log.Error().Err(err).Dur("duration", cmdDuration).Msg("cmd failed")
	} else {
		log.Info().Dur("duration", cmdDuration).Msg("cmd completed")
	}

	if err := clxc.Release(); err != nil {
		log.Error().Err(err).Msg("failed to release container")
	}

	if err != nil {
		if err, yes := err.(execError); yes {
			os.Exit(err.ExitStatus())
		} else {
			// write diagnostics message to stderr for crio/kubelet
			println(err.Error())
			os.Exit(1)
		}
	}

func (c *Runtime) ConfigureLogging(cmdName string) error {
	logDir := filepath.Dir(c.LogFilePath)
	err := os.MkdirAll(logDir, 0750)
	if err != nil {
		return errors.Wrapf(err, "failed to create log file directory %s", logDir)
	}

	c.LogFile, err = os.OpenFile(c.LogFilePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return errors.Wrapf(err, "failed to open log file %s", c.LogFilePath)
	}

	zerolog.LevelFieldName = "l"
	zerolog.MessageFieldName = "m"

	// match liblxc timestamp format
	zerolog.TimestampFieldName = "t"
	zerolog.TimeFieldFormat = timeFormatLXCMillis
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
	log = zerolog.New(c.LogFile).With().Timestamp().Caller().
		Str("cmd", cmdName).Str("cid", c.ContainerID).Logger()

	level, err := zerolog.ParseLevel(strings.ToLower(c.LogLevel))
	if err != nil {
		level = zerolog.InfoLevel
		log.Warn().Err(err).Str("val", c.LogLevel).Stringer("default", level).
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
		return errors.Wrap(err, "failed to set container loglevel")
	}
	if err := c.Container.SetLogFile(c.LogFilePath); err != nil {
		return errors.Wrap(err, "failed to set container log file")
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
		log.Warn().Str("val", c.ContainerLogLevel).
			Stringer("default", lxc.WARN).
			Msg("failed to parse container-log-level - fallback to default")
		return lxc.WARN
	}
}

// executeRuntimeHook executes the runtime hook executable if defined.
// Execution errors are logged at log level error.
func (c *Runtime) executeRuntimeHook(runtimeError error) {
	if c.RuntimeHook == "" {
		return
	}
	env := []string{
		"CONTAINER_ID=" + c.ContainerID,
		"LXC_CONFIG=" + c.ConfigFilePath(),
		"RUNTIME_CMD=" + c.Command,
		"RUNTIME_PATH=" + c.RuntimePath(),
		"BUNDLE_PATH=" + c.BundlePath,
		"SPEC_PATH=" + c.SpecPath(),
		"LOG_FILE=" + c.LogFilePath,
	}

	if runtimeError != nil {
		env = append(env, "RUNTIME_ERROR="+runtimeError.Error())
	}

	log.Debug().Str("file", c.RuntimeHook).Msg("execute runtime hook")
	// TODO drop privileges, capabilities ?
	ctx, cancel := context.WithTimeout(context.Background(), c.RuntimeHookTimeout)
	defer cancel()
	// #nosec
	cmd := exec.CommandContext(ctx, c.RuntimeHook)
	cmd.Env = env
	cmd.Dir = c.RuntimePath()
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("file", c.RuntimeHook).
			Bool("timeout-expired", ctx.Err() == context.DeadlineExceeded).Msg("runtime hook failed")
	}
}

func (c *Runtime) isContainerStopped() bool {
	return c.Container.State() == lxc.STOPPED
}

func (c *Runtime) waitCreated(timeout time.Duration) error {
	if !c.Container.Wait(lxc.RUNNING, timeout) {
		return fmt.Errorf("timeout starting init cmd")
	}

	initState, err := c.getContainerInitState()
	if err != nil {
		return err
	}
	if initState != StateCreated {
		return fmt.Errorf("unexpected init state %q", initState)
	}
	return nil
}

func (c *Runtime) waitRunning(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		initState, err := c.getContainerInitState()
		if err != nil {
			return err
		}
		if initState == StateRunning {
			return nil
		}
		if initState == StateCreated {
			time.Sleep(time.Millisecond * 10)
			continue
		}
		return fmt.Errorf("unexpected init state %q", initState)
	}
	return fmt.Errorf("timeout")
}

// getContainerInitState returns the runtime state of the container.
// It is used to determine whether the container state is 'created' or 'running'.
// The init process environment contains #envStateCreated if the the container
// is created, but not yet running/started.
// This requires the proc filesystem to be mounted on the host.
func (c *Runtime) getContainerState() (ContainerState, error) {
	state := c.Container.State()
	switch state {
	case lxc.STOPPED:
		return StateStopped, nil
	case lxc.STARTING:
		return StateCreating, nil
	case lxc.RUNNING, lxc.STOPPING, lxc.ABORTING, lxc.FREEZING, lxc.FROZEN, lxc.THAWED:
		return c.getContainerInitState()
	default:
		return StateStopped, fmt.Errorf("unsupported lxc container state %q", state)
	}
}

// getContainerInitState returns the detailed state of the container init process.
// If the init process is not running StateStopped is returned along with an error.
func (c *Runtime) getContainerInitState() (ContainerState, error) {
	initPid := c.Container.InitPid()
	if initPid < 1 {
		return StateStopped, fmt.Errorf("init cmd is not running")
	}
	commPath := fmt.Sprintf("/proc/%d/cmdline", initPid)
	cmdline, err := ioutil.ReadFile(commPath)
	if err != nil {
		// can not determine state, caller may try again
		return StateStopped, err
	}

	// comm contains a trailing newline
	initCmdline := fmt.Sprintf("/.crio-lxc/init\000%s\000", c.ContainerID)
	if string(cmdline) == initCmdline {
		//if strings.HasPrefix(c.ContainerID, strings.TrimSpace(string(comm))) {
		return StateCreated, nil
	}
	return StateRunning, nil
}

func enableCgroupControllers(cg string) error {
	//slice := cg.ScopePath()
	// #nosec
	cgPath := filepath.Join("/sys/fs/cgroup", cg)
	if err := os.MkdirAll(cgPath, 755); err != nil {
		return err
	}
	/*
	   // enable all available controllers in the scope
	   data, err := ioutil.ReadFile("/sys/fs/cgroup/cgroup.controllers")
	   if err != nil {
	           return errors.Wrap(err, "failed to read cgroup.controllers")
	   }
	   controllers := strings.Split(strings.TrimSpace(string(data)), " ")

	   var b strings.Builder
	   for i, c := range controllers {
	           if i > 0 {
	                   b.WriteByte(' ')
	           }
	           b.WriteByte('+')
	           b.WriteString(c)
	   }
	   b.WriteString("\n")

	   s := b.String()
	*/
	s := "+cpuset +cpu +io +memory +hugetlb +pids +rdma\n"

	base := "/sys/fs/cgroup"
	for _, elem := range strings.Split(cg, "/") {
		base = filepath.Join(base, elem)
		c := filepath.Join(base, "cgroup.subtree_control")
		err := ioutil.WriteFile(c, []byte(s), 0)
		if err != nil {
			return errors.Wrapf(err, "failed to enable cgroup controllers in %s", base)
		}
		log.Info().Str("file", base).Str("controllers", strings.TrimSpace(s)).Msg("cgroup activated")
	}
	return nil
}

func (c *Runtime) killContainer(signum unix.Signal) error {
	if signum == unix.SIGKILL || signum == unix.SIGTERM {
		if err := c.setConfigItem("lxc.signal.stop", strconv.Itoa(int(signum))); err != nil {
			return err
		}
		if err := c.Container.Stop(); err != nil {
			return err
		}
		if !c.Container.Wait(lxc.STOPPED, time.Second*10) {
			log.Warn().Msg("failed to stop lxc container")
		}

		// draining the cgroup is required to catch processes that escaped from the
		// 'kill' e.g a bash for loop that spawns a new child immediately.
		start := time.Now()
		err := drainCgroup(c.CgroupDir, signum, time.Second*10)
		if err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Str("file", c.CgroupDir).Msg("failed to drain cgroup")
		} else {
			log.Info().Dur("duration", time.Since(start)).Str("file", c.CgroupDir).Msg("cgroup drained")
		}
		return err
	}

	//  send non-terminating signals to monitor process
	pid, err := c.Pid()
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "failed to load pidfile")
	}
	if pid > 1 {
		log.Info().Int("pid", pid).Int("signal", int(signum)).Msg("sending signal")
		if err := unix.Kill(pid, 0); err == nil {
			err := unix.Kill(pid, signum)
			if err != unix.ESRCH {
				return errors.Wrapf(err, "failed to send signal %d to container process %d", signum, pid)
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
			return errors.Wrap(err, "failed to destroy container")
		}
	}

	err := deleteCgroup(c.CgroupDir)
	if err != nil && !os.IsNotExist(err) {
		log.Warn().Err(err).Str("file", c.CgroupDir).Msg("failed to remove cgroup dir")
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
		return errors.Wrap(err, "failed to stat config tmpfile")
	}
	// Don't overwrite an existing config.
	cfgFile := c.ConfigFilePath()
	if _, err := os.Stat(cfgFile); err == nil {
		return fmt.Errorf("config file %s already exists", cfgFile)
	}
	err := c.Container.SaveConfigFile(tmpFile)
	if err != nil {
		return errors.Wrapf(err, "failed to save config file to '%s'", tmpFile)
	}
	if err := os.Rename(tmpFile, cfgFile); err != nil {
		return errors.Wrap(err, "failed to rename config file")
	}
	return nil
}

func (c *Runtime) Start(timeout time.Duration) error {
	log.Info().Msg("notify init to start container process")

	err := c.loadContainer()
	if err != nil {
		return err
	}

	state, err := c.getContainerState()
	if err != nil {
		return err
	}
	if state != StateCreated {
		return fmt.Errorf("invalid container state. expected %q, but was %q", StateCreated, state)
	}

	done := make(chan error)
	go func() {
		done <- c.readFifo()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(time.Second * 5):
		return fmt.Errorf("timeout reading from syncfifo")
	}

	return c.waitRunning(timeout)
}

func (c *Runtime) syncFifoPath() string {
	return c.RuntimePath(initDir, "syncfifo")
}

func (c *Runtime) readFifo() error {
	// #nosec
	f, err := os.OpenFile(c.syncFifoPath(), os.O_RDONLY, 0)
	if err != nil {
		return errors.Wrap(err, "failed to open sync fifo")
	}
	// can not set deadline on fifo
	// #nosec
	defer f.Close()

	data := make([]byte, len(c.ContainerID))
	_, err = f.Read(data)
	if err != nil {
		return errors.Wrap(err, "problem reading from fifo")
	}
	if c.ContainerID != string(data) {
		return errors.Errorf("bad fifo content: %s", string(data))
	}
	return nil
}

func (c *Runtime) Delete(force bool) error {
	err := c.loadContainer()
	if err == errContainerNotExist {
		return nil
	}
	if err != nil {
		return err
	}

	log.Info().Bool("force", force).Msg("delete container")

	if !c.isContainerStopped() {
		if !force {
			return fmt.Errorf("container is not not stopped (current state %s)", c.Container.State())
		}
		if err := c.killContainer(unix.SIGKILL); err != nil {
			return errors.Wrap(err, "failed to kill container")
		}
	}
	return c.destroy()
}

func (c *Runtime) State() (*specs.State, error) {
	err := c.loadContainer()
	if err != nil {
		return nil, err
	}

	pid, err := c.Pid()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to load pidfile")
	}

	state := &specs.State{
		Version:     specs.Version,
		ID:          c.Container.Name(),
		Bundle:      c.BundlePath,
		Pid:         pid,
		Annotations: c.Annotations,
	}

	s, err := c.getContainerState()
	state.Status = string(s)
	if err != nil {
		return nil, err
	}

	log.Info().Int("pid", state.Pid).Str("status", state.Status).Msg("container state")
	return state, nil
}

func (c *Runtime) Kill(signum unix.Signal) error {
	err := c.loadContainer()
	if err != nil {
		return err
	}

	state, err := c.getContainerState()
	if err != nil {
		return err
	}
	if !(state == StateCreated || state == StateRunning) {
		return fmt.Errorf("can only kill container in state Created|Running but was %q", state)
	}
	return c.killContainer(signum)
}

func (c *Runtime) ExecDetached(args []string, proc *specs.Process) (pid int, err error) {
	opts, err := attachOptions(proc, c.Namespaces)
	if err != nil {
		return 0, err
	}

	log.Info().Strs("args", args).
		Int("uid", opts.UID).Int("gid", opts.GID).
		Ints("groups", opts.Groups).Msg("execute cmd")

	return c.Container.RunCommandNoWait(args, opts)
}

func (c *Runtime) Exec(args []string, proc *specs.Process) (exitStatus int, err error) {
	opts, err := attachOptions(proc, c.Namespaces)
	if err != nil {
		return 0, err
	}
	return c.Container.RunCommandStatus(args, opts)
}

// -- LXC helper functions that should be static

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
		return errors.Wrapf(err, "failed to set config item '%s=%s'", key, value)
	}
	log.Debug().Str("lxc.config", key).Str("val", value).Msg("set config item")
	return nil
}

// -- LXC helper functions

func supportsConfigItem(keys ...string) bool {
	for _, key := range keys {
		if !lxc.IsSupportedConfigItem(key) {
			log.Info().Str("lxc.config", key).Msg("unsupported config item")
			return false
		}
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

func ReadSpec(src string) (spec *specs.Spec, err error) {
	err = decodeFileJSON(spec, src)
	return
}

func ReadSpecProcess(src string) (*specs.Process, error) {
	if src == "" {
		return nil, nil
	}
	proc := new(specs.Process)
	err := decodeFileJSON(proc, src)
	return proc, err
}
