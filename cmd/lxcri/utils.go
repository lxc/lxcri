package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func setEnv(key, val string, overwrite bool) error {
	_, exist := os.LookupEnv(key)
	if !exist || overwrite {
		return os.Setenv(key, val)
	}
	return nil
}

func loadEnvFile(envFile string) (map[string]string, error) {
	// don't fail if environment file does not exist
	_, err := os.Stat(envFile)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// #nosec
	data, err := ioutil.ReadFile(envFile)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	env := make(map[string]string, len(lines))
	for n, line := range lines {
		trimmed := strings.TrimSpace(line)
		// skip over comments and blank lines
		if len(trimmed) == 0 || trimmed[0] == '#' {
			continue
		}
		vals := strings.SplitN(trimmed, "=", 2)
		if len(vals) != 2 {
			return nil, fmt.Errorf("invalid environment variable at line %s:%d", envFile, n+1)
		}
		key := strings.TrimSpace(vals[0])
		val := strings.Trim(strings.TrimSpace(vals[1]), `"'`)
		env[key] = val
	}
	return env, nil
}

func parseSignal(sig string) unix.Signal {
	if sig == "" {
		return unix.SIGTERM
	}
	// handle numerical signal value
	if num, err := strconv.Atoi(sig); err == nil {
		return unix.Signal(num)
	}

	// gracefully handle all string variants e.g 'sigkill|SIGKILL|kill|KILL'
	s := strings.ToUpper(sig)
	if !strings.HasPrefix(s, "SIG") {
		s = "SIG" + s
	}
	return unix.SignalNum(s)
}

/*
func logEnv(log *zerolog.Log)
		if env != nil {
			stat, _ := os.Stat(envFile)
			if stat != nil && (stat.Mode().Perm()^0640) != 0 {
				log.Warn().Str("file", envFile).Stringer("mode", stat.Mode().Perm()).Msgf("environment file should have mode %s", os.FileMode(0640))
			}
			for key, val := range env {
				log.Trace().Str("env", key).Str("val", val).Msg("environment file value")
			}
			log.Debug().Str("file", envFile).Msg("loaded environment variables from file")
		} else {
			if os.IsNotExist(envErr) {
				log.Warn().Str("file", envFile).Msg("environment file does not exist")
			} else {
				return errors.Wrapf(envErr, "failed to load env file %s", envFile)
			}
		}

		for _, f := range ctx.Command.Flags {
			name := f.Names()[0]
			log.Trace().Str("flag", name).Str("val", ctx.String(name)).Msg("flag value")
		}

  }
*/

// createPidFile atomically creates a pid file for the given pid at the given path
func createPidFile(path string, pid int) error {
	tmpDir := filepath.Dir(path)
	tmpName := filepath.Join(tmpDir, fmt.Sprintf(".%s", filepath.Base(path)))

	// #nosec
	f, err := os.OpenFile(tmpName, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create temporary PID file %q: %w", tmpName, err)
	}
	_, err = fmt.Fprintf(f, "%d", pid)
	if err != nil {
		return fmt.Errorf("failed to write to temporary PID file %q: %w", tmpName, err)
	}
	err = f.Close()
	if err != nil {
		return fmt.Errorf("failed to close temporary PID file %q: %w", tmpName, err)
	}
	err = os.Rename(tmpName, path)
	if err != nil {
		return fmt.Errorf("failed to rename temporary PID file %q to %q: %w", tmpName, path, err)
	}
	return nil
}

func readPidFile(path string) (int, error) {
	// #nosec
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(data))
	return strconv.Atoi(s)
}
