package main

import (
	"context"
	"encoding/json"
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
	timeFormatLXCMillis      = "20060102150405.000"
	defaultContainerLogLevel = lxc.WARN
	defaultLogLevel          = zerolog.WarnLevel

	// ConfigDir is the path to the crio-lxc resources relative to the container rootfs.
	configDir = "/.crio-lxc"
	// SyncFifoPath is the path to the fifo used to block container start in init until start cmd is called.
	syncFifoPath = configDir + "/syncfifo"
	// InitCmd is the path where the init binary is bind mounted.
	initCmd = configDir + "/init"
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

// The singelton that wraps the lxc.Container
var clxc crioLXC
var log zerolog.Logger

var errContainerNotExist = errors.New("container does not exist")
var errContainerExist = errors.New("container already exists")

type crioLXC struct {
	Container *lxc.Container

	Command string

	// [ global settings ]
	RuntimeRoot       string
	ContainerID       string
	LogFile           *os.File
	LogFilePath       string
	LogLevel          string
	ContainerLogLevel string
	SystemdCgroup     bool

	StartCommand         string
	InitCommand          string
	ContainerHook        string
	RuntimeHook          string
	RuntimeHookTimeout   time.Duration
	RuntimeHookRunAlways bool

	// feature gates
	Seccomp       bool
	Capabilities  bool
	Apparmor      bool
	CgroupDevices bool

	// create flags
	ConsoleSocketTimeout time.Duration

	// start flags
	StartTimeout time.Duration

	bundleConfig
}

type bundleConfig struct {
	BundlePath    string
	SpecPath      string // BundlePath + "/config.json"
	PidFile       string
	ConsoleSocket string
	MonitorCgroup string
	// values derived from spec
	CgroupsPath string
}

var version string

func versionString() string {
	return fmt.Sprintf("%s (%s) (lxc:%s)", version, runtime.Version(), lxc.Version())
}

// runtimePath builds an absolute filepath which is relative to the container runtime root.
func (c *crioLXC) runtimePath(subPath ...string) string {
	return filepath.Join(c.RuntimeRoot, c.ContainerID, filepath.Join(subPath...))
}
func (c *crioLXC) bundlePath(subPath ...string) string {
	return filepath.Join(c.BundlePath, filepath.Join(subPath...))
}

func (c *crioLXC) configFilePath() string {
	return c.runtimePath("config")
}

func (c *crioLXC) readPidFile() (int, error) {
	// #nosec
	data, err := ioutil.ReadFile(c.PidFile)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(data))
	return strconv.Atoi(s)
}

func (c *crioLXC) createPidFile(pid int) error {
	return createPidFile(c.PidFile, pid)
}

// ReadSpec deserializes the JSON encoded runtime spec from the given path.
func (c *crioLXC) readSpec() (*specs.Spec, error) {
	// #nosec
	// FIXME set this once
	c.SpecPath = c.bundlePath("config.json")

	specFile, err := os.Open(c.SpecPath)
	if err != nil {
		return nil, err
	}
	// #nosec
	defer specFile.Close()
	spec := &specs.Spec{}
	err = json.NewDecoder(specFile).Decode(spec)
	if err != nil {
		return nil, err
	}

	if spec.Linux.CgroupsPath == "" {
		return nil, fmt.Errorf("empty cgroups path in spec")
	}
	if c.SystemdCgroup {
		c.CgroupsPath = parseSystemdCgroupPath(spec.Linux.CgroupsPath)
	} else {
		c.CgroupsPath = spec.Linux.CgroupsPath
	}

	return spec, nil
}

// loadContainer checks for the existence of the lxc config file.
// It returns an error if the config file does not exist.
func (c *crioLXC) loadContainer() error {
	_, err := os.Stat(c.configFilePath())
	if os.IsNotExist(err) {
		return errContainerNotExist
	}
	if err != nil {
		return errors.Wrap(err, "failed to access config file")
	}

	container, err := lxc.NewContainer(c.ContainerID, c.RuntimeRoot)
	if err != nil {
		return errors.Wrap(err, "failed to load container")
	}
	err = container.LoadConfigFile(c.configFilePath())
	if err != nil {
		return errors.Wrap(err, "failed to load config file")
	}
	c.Container = container

	if err := c.setContainerLogLevel(); err != nil {
		return err
	}

	return c.readBundleConfig()
}

// createContainer creates a new container.
// It must only be called once during the lifecycle of a container.
func (c *crioLXC) createContainer() error {
	if _, err := os.Stat(c.configFilePath()); err == nil {
		return errContainerExist
	}

	if err := os.MkdirAll(c.runtimePath(), 0700); err != nil {
		return errors.Wrap(err, "failed to create container dir")
	}

	// An empty tmpfile is created to ensure that createContainer can only succeed once.
	// The config file is atomically activated in saveConfig.
	// #nosec
	f, err := os.OpenFile(c.runtimePath(".config"), os.O_EXCL|os.O_CREATE|os.O_RDWR, 0640)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return errors.Wrap(err, "failed to close empty config file")
	}

	c.MonitorCgroup = filepath.Join(c.MonitorCgroup, c.ContainerID+".scope")

	if err := c.writeBundleConfig(); err != nil {
		return err
	}

	container, err := lxc.NewContainer(c.ContainerID, c.RuntimeRoot)
	if err != nil {
		return err
	}
	c.Container = container
	return c.setContainerLogLevel()
}

func (c *crioLXC) configureCgroupPath() error {
	if err := clxc.setConfigItem("lxc.cgroup.relative", "0"); err != nil {
		return err
	}

	// @since lxc @a900cbaf257c6a7ee9aa73b09c6d3397581d38fb
	// checking for on of the config items shuld be enough, because they were introduced together ...
	if supportsConfigItem("lxc.cgroup.dir.container", "lxc.cgroup.dir.monitor") {
		if err := c.setConfigItem("lxc.cgroup.dir.container", c.CgroupsPath); err != nil {
			return err
		}
		if err := c.setConfigItem("lxc.cgroup.dir.monitor", c.MonitorCgroup); err != nil {
			return err
		}
	} else {
		if err := c.setConfigItem("lxc.cgroup.dir", c.CgroupsPath); err != nil {
			return err
		}
	}
	if supportsConfigItem("lxc.cgroup.dir.monitor.pivot") {
		if err := c.setConfigItem("lxc.cgroup.dir.monitor.pivot", c.MonitorCgroup); err != nil {
			return err
		}
	}
	return nil
}

func (c *crioLXC) readBundleConfig() error {
	p := c.runtimePath("bundle.json")
	data, err := ioutil.ReadFile(p)
	if err != nil {
		return errors.Wrapf(err, "failed to read bundle config file %s", p)
	}
	err = json.Unmarshal(data, &c.bundleConfig)
	if err != nil {
		return errors.Wrap(err, "failed to unmarshal bundle config")
	}
	return nil
}

func (c *crioLXC) writeBundleConfig() error {
	p := c.runtimePath("bundle.json")
	f, err := os.OpenFile(p, os.O_EXCL|os.O_CREATE|os.O_RDWR, 0640)
	if err != nil {
		return errors.Wrapf(err, "failed to create bundle config file %s", p)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	err = enc.Encode(c.bundleConfig)
	if err != nil {
		f.Close()
		return errors.Wrap(err, "failed to marshal bundle config")
	}
	return f.Close()
}

// saveConfig creates and atomically enables the lxc config file.
// It must be called after #createContainer and only once.
// Any config changes via clxc.setConfigItem must be done
// before calling saveConfig.
func (c *crioLXC) saveConfig() error {
	// createContainer creates the tmpfile
	tmpFile := c.runtimePath(".config")
	if _, err := os.Stat(tmpFile); err != nil {
		return errors.Wrap(err, "failed to stat config tmpfile")
	}
	// Don't overwrite an existing config.
	cfgFile := c.configFilePath()
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
	log.Debug().Str("file", cfgFile).Msg("created lxc config file")
	return nil
}

// Release releases/closes allocated resources (lxc.Container, LogFile)
func (c crioLXC) release() error {
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

func supportsConfigItem(keys ...string) bool {
	for _, key := range keys {
		if !lxc.IsSupportedConfigItem(key) {
			log.Info().Str("lxc.config", key).Msg("unsupported config item")
			return false
		}
	}
	return true
}

func (c *crioLXC) getConfigItem(key string) string {
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

func (c *crioLXC) setConfigItem(key, value string) error {
	err := c.Container.SetConfigItem(key, value)
	if err != nil {
		return errors.Wrapf(err, "failed to set config item '%s=%s'", key, value)
	}
	log.Debug().Str("lxc.config", key).Str("val", value).Msg("set config item")
	return nil
}

func (c *crioLXC) configureLogging() error {
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
		Str("cmd", c.Command).Str("cid", c.ContainerID).Logger()

	level, err := zerolog.ParseLevel(strings.ToLower(c.LogLevel))
	if err != nil {
		level = defaultLogLevel
		log.Warn().Err(err).Str("val", c.LogLevel).Stringer("default", level).
			Msg("failed to parse log-level - fallback to default")
	}
	zerolog.SetGlobalLevel(level)
	return nil
}

func (c *crioLXC) setContainerLogLevel() error {
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

func (c *crioLXC) parseContainerLogLevel() lxc.LogLevel {
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
			Stringer("default", defaultContainerLogLevel).
			Msg("failed to parse container-log-level - fallback to default")
		return defaultContainerLogLevel
	}
}

func (c *crioLXC) executeRuntimeHook(err error) {
	if c.RuntimeHook == "" {
		return
	}
	// prepare environment
	env := []string{
		"CONTAINER_ID=" + c.ContainerID,
		"LXC_CONFIG=" + c.configFilePath(),
		"RUNTIME_CMD=" + c.Command,
		"RUNTIME_PATH=" + c.runtimePath(),
		"BUNDLE_PATH=" + c.BundlePath,
		"SPEC_PATH=" + c.SpecPath,
		"LOG_FILE=" + c.LogFilePath,
	}

	if err != nil {
		env = append(env, "RUNTIME_ERROR="+err.Error())
	}

	log.Debug().Str("file", clxc.RuntimeHook).Msg("execute runtime hook")
	// TODO drop privileges, capabilities ?
	ctx, cancel := context.WithTimeout(context.Background(), clxc.RuntimeHookTimeout)
	defer cancel()
	// #nosec
	cmd := exec.CommandContext(ctx, c.RuntimeHook)
	cmd.Env = env
	cmd.Dir = "/"
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("file", c.RuntimeHook).
			Bool("timeout-expired", ctx.Err() == context.DeadlineExceeded).Msg("runtime hook failed")
	}
}

func (c *crioLXC) isContainerStopped() bool {
	return c.Container.State() == lxc.STOPPED
}

// getContainerInitState returns the runtime state of the container.
// It is used to determine whether the container state is 'created' or 'running'.
// The init process environment contains #envStateCreated if the the container
// is created, but not yet running/started.
// This requires the proc filesystem to be mounted on the host.
func (c *crioLXC) getContainerState() (ContainerState, error) {
	state := c.Container.State()
	switch state {
	case lxc.STOPPED:
		return StateStopped, nil
	case lxc.STARTING:
		return StateCreating, nil
	}
	// RUNNING, STOPPING, ABORTING, FREEZING, FROZEN, THAWED:
	pid := c.Container.InitPid()
	if pid < 0 {
		return StateCreating, nil
	}

	commPath := fmt.Sprintf("/proc/%d/cmdline", pid)
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

func (c *crioLXC) killContainer(signum unix.Signal) error {
	pid, err := c.readPidFile()
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "failed to load pidfile")
	}
	log.Info().Int("pid", pid).Int("signal", int(signum)).Msg("sending signal")

	// send signal to the monitor process if it still exist
	// signals other than SIGTERM are forwarded from liblxc to the container int process
	if err := unix.Kill(pid, 0); err == nil {
		err := unix.Kill(pid, signum)
		// container process has already died
		if signum == unix.SIGKILL || signum == unix.SIGTERM {
			return nil
		}
		return err
	}
	return nil
}

func (c *crioLXC) destroy() error {
	if c.Container != nil {
		if err := c.Container.Destroy(); err != nil {
			return errors.Wrap(err, "failed to destroy container")
		}
	}

	// cgroup directories must be removed if container process was killed with SIGKILL
	c.tryRemoveCgroups()

	// "Note that resources associated with the container,
	// but not created by this container, MUST NOT be deleted."
	// TODO - because we set rootfs.managed=0, Destroy() doesn't
	// delete the /var/lib/lxc/$containerID/config file:

	return os.RemoveAll(c.runtimePath())
}

func (c *crioLXC) tryRemoveCgroups() {
	if err := deleteCgroup(c.CgroupsPath); err != nil {
		log.Warn().Err(err).Str("cgroup", c.CgroupsPath).Msg("failed to remove cgroup")
	}
	if err := deleteCgroup(c.MonitorCgroup); err != nil {
		log.Warn().Err(err).Str("cgroup", c.MonitorCgroup).Msg("failed to remove monitor cgroup")
	}
}
