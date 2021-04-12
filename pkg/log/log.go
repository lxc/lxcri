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

func init() {
	zerolog.LevelFieldName = "l"
	zerolog.MessageFieldName = "m"

	// match liblxc timestamp format
	zerolog.TimestampFieldName = "t"
	//zerolog.TimeFieldFormat = "20060102150405.000"
	zerolog.TimestampFunc = func() time.Time {
		return time.Now().UTC()
	}

	zerolog.CallerFieldName = "c"
	zerolog.CallerMarshalFunc = func(file string, line int) string {
		return filepath.Base(file) + ":" + strconv.Itoa(line)
	}
}

// OpenFile opens a new or appends to an existing log file.
// The parent directory is created if it does not exist.
func OpenFile(name string, mode os.FileMode) (*os.File, error) {
	logDir := filepath.Dir(name)
	err := os.MkdirAll(logDir, 0750)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file directory %s: %w", logDir, err)
	}
	return os.OpenFile(name, os.O_WRONLY|os.O_APPEND|os.O_CREATE, mode)
}

// ParseLevel is a wrapper for zerolog.ParseLevel
func ParseLevel(level string) (zerolog.Level, error) {
	return zerolog.ParseLevel(strings.ToLower(level))
}

// NewLogger creates a new zerlog.Context from the given arguments.
// The returned context is configured to log with timestamp and caller information.
func NewLogger(out io.Writer, level zerolog.Level) zerolog.Context {
	// NOTE Unfortunately it's not possible change the possition of the timestamp.
	// The ttimestamp is appended to the to the log output because it is dynamically rendered
	// see https://github.com/rs/zerolog/issues/109
	return zerolog.New(out).Level(level).With().Timestamp().Caller()
}

// ConsoleLogger returns a new zerlog.Logger suited for console usage (e.g unit tests)
func ConsoleLogger(color bool) zerolog.Logger {
	return zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, NoColor: !color}).Level(zerolog.DebugLevel).With().Timestamp().Caller().Logger()
}
