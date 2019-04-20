package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"github.com/urfave/cli"

	lxc "gopkg.in/lxc/go-lxc.v2"
)

func readBundleSpec(specFilePath string) (spec *specs.Spec, err error) {
	specFile, err := os.Open(specFilePath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open spec file '%s'", specFilePath)
	}
	defer specFile.Close()
	err = json.NewDecoder(specFile).Decode(&spec)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to decode spec file")
	}

	return spec, nil
}

func configureLogging(ctx *cli.Context, c *lxc.Container) error {
	if ctx.GlobalIsSet("log-level") {
		logLevel := lxc.TRACE
		switch ctx.GlobalString("log-level") {
		case "trace":
			logLevel = lxc.TRACE
		case "debug":
			logLevel = lxc.DEBUG
		case "info":
			logLevel = lxc.INFO
		case "warn":
			logLevel = lxc.WARN
		case "", "error":
			logLevel = lxc.ERROR
		default:
			return fmt.Errorf("lxc driver config 'log_level' can only be trace, debug, info, warn or error")
		}
		c.SetLogLevel(logLevel)
	}

	if ctx.GlobalIsSet("log-file") {
		c.SetLogFile(ctx.GlobalString("log-file"))
	}
	return nil
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return true, err
}

func containerExists(containerID string) (bool, error) {
	// check for container existence by looking for config file.
	// otherwise NewContainer will return an empty container
	// struct and we'll report wrong info
	configExists, err := pathExists(filepath.Join(LXC_PATH, containerID, "config"))
	if err != nil {
		return false, errors.Wrap(err, "failed to check path existence of config")
	}

	return configExists, nil
}

func RunCommand(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Errorf("%s: %s: %s", strings.Join(args, " "), err, string(output))
	}
	return nil
}
