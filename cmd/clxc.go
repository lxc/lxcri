package main

import (
	"fmt"
	"github.com/pkg/errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rs/zerolog"
	"gopkg.in/lxc/go-lxc.v2"
)

// time format used for logger
const TimeFormatLXCMillis = "20060102150405.000"

var log zerolog.Logger

var ErrExist = errors.New("container already exists")
var ErrContainerNotExist = errors.New("container does not exist")

type CrioLXC struct {
	*lxc.Container

	Command string

	// [ global settings ]
	RuntimeRoot    string
	ContainerID    string
	LogFile        *os.File
	LogFilePath    string
	LogLevel       lxc.LogLevel
	LogLevelString string
}

func (c CrioLXC) VersionString() string {
	return fmt.Sprintf("%s (%s) (lxc:%s)", version, runtime.Version(), lxc.Version())
}

// RuntimePath builds an absolute filepath which is relative to the container runtime root.
func (c *CrioLXC) RuntimePath(subPath ...string) string {
	return filepath.Join(c.RuntimeRoot, c.ContainerID, filepath.Join(subPath...))
}

func (c *CrioLXC) LoadContainer() error {
	// Check for container existence by looking for config file.
	// Otherwise lxc.NewContainer will return an empty container
	// struct and we'll report wrong info
	_, err := os.Stat(c.RuntimePath("config"))
	if os.IsNotExist(err) {
		return ErrContainerNotExist
	}
	if err != nil {
		return errors.Wrap(err, "failed to access config file")
	}

	container, err := lxc.NewContainer(c.ContainerID, c.RuntimeRoot)
	if err != nil {
		return errors.Wrap(err, "failed to load container")
	}
	if err := container.LoadConfigFile(c.RuntimePath("config")); err != nil {
		return err
	}
	c.Container = container
	return nil
}

func (c *CrioLXC) CreateContainer() error {
	_, err := os.Stat(c.RuntimePath("config"))
	if !os.IsNotExist(err) {
		return ErrExist
	}
	container, err := lxc.NewContainer(c.ContainerID, c.RuntimeRoot)
	if err != nil {
		return err
	}
	c.Container = container
	if err := os.MkdirAll(c.RuntimePath(), 0770); err != nil {
		return errors.Wrap(err, "failed to create container dir")
	}
	return nil
}

// Release releases/closes allocated resources (lxc.Container, LogFile)
func (c CrioLXC) Release() {
	if c.Container != nil {
		c.Container.Release()
	}
	if c.LogFile != nil {
		c.LogFile.Close()
	}
}

func (c CrioLXC) CanConfigure(keys ...string) bool {
	for _, key := range keys {
		if !lxc.IsSupportedConfigItem(key) {
			log.Info().Str("key:", key).Msg("unsupported lxc config item")
			return false
		}
	}
	return true
}

func (c *CrioLXC) GetConfigItem(key string) string {
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

func (c *CrioLXC) SetConfigItem(key, value string) error {
	err := c.Container.SetConfigItem(key, value)
	if err != nil {
		log.Error().Err(err).Str("key:", key).Str("value:", value).Msg("lxc config")
	} else {
		log.Debug().Str("key:", key).Str("value:", value).Msg("lxc config")
	}
	return errors.Wrap(err, "failed to set lxc config item '%s=%s'")
}

func (c *CrioLXC) configureLogging() error {
	logDir := filepath.Dir(c.LogFilePath)
	err := os.MkdirAll(logDir, 0750)
	if err != nil {
		return errors.Wrapf(err, "failed to create log file directory %s", logDir)
	}

	f, err := os.OpenFile(c.LogFilePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0640)
	if err != nil {
		return errors.Wrapf(err, "failed to open log file %s", c.LogFilePath)
	}
	c.LogFile = f

	zerolog.TimestampFieldName = "t"
	zerolog.LevelFieldName = "p"
	zerolog.MessageFieldName = "m"
	zerolog.TimeFieldFormat = TimeFormatLXCMillis

	// NOTE It's not possible change the possition of the timestamp.
	// The ttimestamp is appended to the to the log output because it is dynamically rendered
	// see https://github.com/rs/zerolog/issues/109
	log = zerolog.New(f).With().Timestamp().Str("cmd:", c.Command).Str("cid:", c.ContainerID).Logger()

	level, err := parseLogLevel(c.LogLevelString)
	if err != nil {
		log.Error().Err(err).Stringer("loglevel:", level).Msg("using fallback log-level")
	}
	c.LogLevel = level

	switch level {
	case lxc.TRACE:
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	case lxc.DEBUG:
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case lxc.INFO:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case lxc.WARN:
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case lxc.ERROR:
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	}
	return nil
}

func parseLogLevel(s string) (lxc.LogLevel, error) {
	switch strings.ToLower(s) {
	case "trace":
		return lxc.TRACE, nil
	case "debug":
		return lxc.DEBUG, nil
	case "info":
		return lxc.INFO, nil
	case "warn":
		return lxc.WARN, nil
	case "error":
		return lxc.ERROR, nil
	default:
		return lxc.ERROR, fmt.Errorf("Invalid log-level %s", s)
	}
}
