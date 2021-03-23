package lxcontainer

import (
	"fmt"
	"github.com/rs/zerolog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Log struct {
	zerolog.Logger `json:"-"`
	File           *os.File `json:"-"`
	FilePath       string
	Level          string
	Timestamp      string
	LevelContainer string
}

func (log Log) Open(cmdName string) error {
	logDir := filepath.Dir(log.FilePath)
	err := os.MkdirAll(logDir, 0750)
	if err != nil {
		return fmt.Errorf("failed to create log file directory %s: %w", logDir, err)
	}

	log.File, err = os.OpenFile(log.FilePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return err
	}

	zerolog.LevelFieldName = "l"
	zerolog.MessageFieldName = "m"

	// match liblxc timestamp format
	zerolog.TimestampFieldName = "t"
	zerolog.TimeFieldFormat = log.Timestamp
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
	// FIXME ContainerID is not part of the runtime anymore
	//	rt.Log = zerolog.New(rt.LogFile).With().Timestamp().Caller().
	//		Str("cmd", cmdName).Str("cid", c.ContainerID).Logger()
	log.Logger = zerolog.New(log.File).With().Timestamp().Caller().
		Str("cmd", cmdName).Logger()

	level, err := zerolog.ParseLevel(strings.ToLower(log.Level))
	if err != nil {
		level = zerolog.InfoLevel
		log.Warn().Err(err).Str("val", log.Level).Stringer("default", level).
			Msg("failed to parse log-level - fallback to default")
	}
	zerolog.SetGlobalLevel(level)
	return nil
}

func (log Log) Close() error {
	return log.File.Close()
}

// FIXME move to test utils ?
func testLogger() zerolog.Logger {
	z := zerolog.New(os.Stderr)
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	return z
}
