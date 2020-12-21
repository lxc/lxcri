package main

import (
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/lxc/crio-lxc/lxcontainer"
	"github.com/urfave/cli/v2"
)

func setEnv(key, val string, overwrite bool) error {
	 _, exist := os.LookupEnv(key)
	 if !exist || overwrite {
		return os.Setenv(key, val)
	}
	return nil
}

func loadEnvFile(envFile string) (map[string]string, error) {
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
