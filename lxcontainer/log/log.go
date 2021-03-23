package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// TODO log to systemd journal ?

func init() {
	zerolog.LevelFieldName = "l"
	zerolog.MessageFieldName = "m"

	// match liblxc timestamp format
	zerolog.TimestampFieldName = "t"
	//zerolog.TimeFieldFormat = "20060102150405.000"
	zerolog.TimestampFunc = func() time.Time {
		return time.Now().UTC()
	}

	// TODO only log caller information in debug and trace level
	zerolog.CallerFieldName = "c"
	zerolog.CallerMarshalFunc = func(file string, line int) string {
		return filepath.Base(file) + ":" + strconv.Itoa(line)
	}
}

func OpenFile(name string, mode os.FileMode) (*os.File, error) {
	logDir := filepath.Dir(name)
	err := os.MkdirAll(logDir, 0750)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file directory %s: %w", logDir, err)
	}
	return os.OpenFile(name, os.O_WRONLY|os.O_APPEND|os.O_CREATE, mode)
}

func ParseLevel(level string) (zerolog.Level, error) {
	return zerolog.ParseLevel(strings.ToLower(level))
}

func NewLogger(out io.Writer, level zerolog.Level) zerolog.Context {
	// NOTE Unfortunately it's not possible change the possition of the timestamp.
	// The ttimestamp is appended to the to the log output because it is dynamically rendered
	// see https://github.com/rs/zerolog/issues/109
	return zerolog.New(out).Level(level).With().Timestamp().Caller()
}

func NewTestLogger(color bool) zerolog.Logger {
	return zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, NoColor: !color}).Level(zerolog.DebugLevel).With().Timestamp().Caller().Logger()
}
