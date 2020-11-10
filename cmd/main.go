package main

import (
	"fmt"
	"github.com/pkg/errors"
	"os"

	"github.com/urfave/cli/v2"
)

const (
	// IMPORTANT should be synced with the runtime-spec dependency in go.mod
	// github.com/opencontainers/runtime-spec v1.0.2
	CURRENT_OCI_VERSION = "1.0.2"
)

var version string
var clxc CrioLXC

func main() {
	app := cli.NewApp()
	app.Name = "crio-lxc"
	app.Usage = "crio-lxc is a CRI compliant runtime wrapper for lxc"
	app.Version = clxc.VersionString()
	app.Commands = []*cli.Command{
		&stateCmd,
		&createCmd,
		&startCmd,
		&killCmd,
		&deleteCmd,
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "log-level",
			Usage:       "set log level (trace|debug|info|warn|error)",
			EnvVars:     []string{"CRIO_LXC_LOG_LEVEL"},
			Value:       "warn",
			Destination: &clxc.LogLevelString,
		},
		&cli.StringFlag{
			Name:        "log-file",
			Usage:       "log file for LXC and crio-lxc (default is per container in lxc-path)",
			EnvVars:     []string{"CRIO_LXC_LOG_FILE", "LOG_FILE"},
			Value:       "/var/log/crio-lxc.log",
			Destination: &clxc.LogFilePath,
		&cli.StringFlag{
			Name:        "root",
			Aliases:     []string{"lxc-path"}, // 'root' is used by crio/conmon
			Usage:       "set the root path where container resources are created (logs, init and hook scripts). Must have access permissions",
			Value:       "/var/lib/lxc",
			Destination: &clxc.RuntimeRoot,
		},
	}


	app.Before = func(ctx *cli.Context) error {
		clxc.Command = ctx.Args().Get(0)
		return nil
	}

	setupCmd := func(ctx *cli.Context) error {
		containerID := ctx.Args().Get(0)
		if len(containerID) == 0 {
			return errors.New("missing container ID")
		}
		clxc.ContainerID = containerID
		clxc.Command = ctx.Command.Name

		if err := clxc.configureLogging(); err != nil {
			return err
		}

		for _, env := range os.Environ() {
			log.Trace().Str("env:", env).Msg("effective environment variable")
		}
		for _, appFlag := range app.Flags {
			name := appFlag.Names()[0]
			log.Trace().Str("name:", name).Str("value:", ctx.String(name)).Msg("effective cmdline flag")
		}

		log.Info().Strs("args", os.Args).Msg("run cmd")
		return nil
	}

	// Disable the default error messages for cmdline errors.
	// By default the app/cmd help is printed to stdout, which produces garbage in cri-o log output.
	// Instead the cmdline is reflected to identify cmdline interface errors
	errUsage := func(context *cli.Context, err error, isSubcommand bool) error {
		fmt.Fprintf(os.Stderr, "usage error %s: %s\n", err, os.Args)
		return err
	}
	app.OnUsageError = errUsage

	for _, cmd := range app.Commands {
		cmd.Before = setupCmd
		cmd.OnUsageError = errUsage
	}

	app.CommandNotFound = func(ctx *cli.Context, cmd string) {
		fmt.Fprintf(os.Stderr, "undefined subcommand %q cmdline%s\n", cmd, os.Args)
	}

	err := app.Run(os.Args)

	clxc.Release()
	if err != nil {
		// write diagnostics message to stderr for crio/kubelet
		println(err.Error())
		os.Exit(1)
	}
}
