package main

import (
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v2"
)

// Environment variables are populated by default from this environment file.
// Existing environment variables are preserved.
var envFile = "/etc/default/crio-lxc"

func main() {
	app := cli.NewApp()
	app.Name = "crio-lxc"
	app.Usage = "crio-lxc is a CRI compliant runtime wrapper for lxc"
	app.Version = versionString()
	// The default handler will exit the appp if the command returns an error
	// that implements the cli.ExitCoder interface.
	// E.g an unwrapped error from os.Exec
	app.ExitErrHandler = func(context *cli.Context, err error) {}
	app.Commands = []*cli.Command{
		&stateCmd,
		&createCmd,
		&startCmd,
		&killCmd,
		&deleteCmd,
		&execCmd,
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "log-level",
			Usage:       "set the runtime log level (trace|debug|info|warn|error)",
			EnvVars:     []string{"CRIO_LXC_LOG_LEVEL"},
			Value:       defaultLogLevel.String(),
			Destination: &clxc.LogLevel,
		},
		&cli.StringFlag{
			Name:        "container-log-level",
			Usage:       "set the container (liblxc) log level (trace|debug|info|notice|warn|error|crit|alert|fatal)",
			EnvVars:     []string{"CRIO_LXC_CONTAINER_LOG_LEVEL"},
			Value:       defaultContainerLogLevel.String(),
			Destination: &clxc.ContainerLogLevel,
		},
		&cli.StringFlag{
			Name:        "log-file",
			Usage:       "path to log-file for combined runtime and container output",
			EnvVars:     []string{"CRIO_LXC_LOG_FILE"},
			Value:       "/var/log/crio-lxc.log",
			Destination: &clxc.LogFilePath,
		},
		&cli.StringFlag{
			Name:        "backup-dir",
			Usage:       "directory for container runtime directory backups",
			EnvVars:     []string{"CRIO_LXC_BACKUP_DIR"},
			Value:       "/var/lib/crio-lxc/backup",
			Destination: &clxc.BackupDir,
		},
		&cli.BoolFlag{
			Name:        "backup-on-error",
			Usage:       "backup container runtime directory when cmd-start fails",
			EnvVars:     []string{"CRIO_LXC_BACKUP_ON_ERROR"},
			Value:       true,
			Destination: &clxc.BackupOnError,
		},
		&cli.BoolFlag{
			Name:        "backup",
			Usage:       "backup every container runtime before cmd-start is called",
			EnvVars:     []string{"CRIO_LXC_BACKUP"},
			Value:       false,
			Destination: &clxc.Backup,
		},
		&cli.StringFlag{
			Name:  "root",
			Usage: "container runtime root where (logs, init and hook scripts). tmpfs is recommended.",
			// exec permissions are not required because init is bind mounted into the root
			Value:       "/run/crio-lxc",
			Destination: &clxc.RuntimeRoot,
		},
		&cli.BoolFlag{
			Name:        "systemd-cgroup",
			Usage:       "enable systemd cgroup",
			Destination: &clxc.SystemdCgroup,
		},
		&cli.StringFlag{
			Name:        "monitor-cgroup",
			Usage:       "cgroup for LXC monitor processes",
			Destination: &clxc.MonitorCgroup,
			EnvVars:     []string{"CRIO_LXC_MONITOR_CGROUP"},
			Value:       "crio-lxc-monitor.slice",
		},
		&cli.StringFlag{
			Name:        "cmd-init",
			Usage:       "Absolute path to container init binary",
			EnvVars:     []string{"CRIO_LXC_CMD_INIT"},
			Value:       "/usr/local/bin/crio-lxc-init",
			Destination: &clxc.InitCommand,
		},
		&cli.StringFlag{
			Name:        "cmd-start",
			Usage:       "Name or path to container start binary",
			EnvVars:     []string{"CRIO_LXC_CMD_START"},
			Value:       "crio-lxc-start",
			Destination: &clxc.StartCommand,
		},
		&cli.StringFlag{
			Name:        "cmd-hook",
			Usage:       "Name or path to binary executed in lxc.hook.mount",
			EnvVars:     []string{"CRIO_LXC_CMD_HOOK"},
			Value:       "/usr/local/bin/crio-lxc-hook",
			Destination: &clxc.HookCommand,
		},
		&cli.BoolFlag{
			Name:        "seccomp",
			Usage:       "Generate and apply seccomp profile for lxc from container spec",
			Destination: &clxc.Seccomp,
			EnvVars:     []string{"CRIO_LXC_SECCOMP"},
			Value:       true,
		},
		&cli.BoolFlag{
			Name:        "capabilities",
			Usage:       "Keep capabilities defined in container spec",
			Destination: &clxc.Capabilities,
			EnvVars:     []string{"CRIO_LXC_CAPABILITIES"},
			Value:       true,
		},
		&cli.BoolFlag{
			Name:        "apparmor",
			Usage:       "Set apparmor profile defined in container spec",
			Destination: &clxc.Apparmor,
			EnvVars:     []string{"CRIO_LXC_APPARMOR"},
			Value:       true,
		},
		&cli.BoolFlag{
			Name:        "cgroup-devices",
			Usage:       "Allow only devices permitted by container spec",
			Destination: &clxc.CgroupDevices,
			EnvVars:     []string{"CRIO_LXC_CGROUP_DEVICES"},
			Value:       true,
		},
	}

	startTime := time.Now()

	env, envErr := loadEnvFile(envFile)
	// Environment variables must be injected from file before app is run,
	// Otherwise the values are not set to the crioLXC instance.
	if env != nil {
		for key, val := range env {
			if err := setEnvIfNew(key, val); err != nil {
				envErr = err
				break
			}
		}
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

		log.Debug().Strs("args", os.Args).Msg("run cmd")
		return nil
	}

	// Disable the default error messages for cmdline errors.
	// By default the app/cmd help is printed to stdout, which produces garbage in cri-o log output.
	// Instead the cmdline is printed to stderr to identify cmdline interface errors.
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
	cmdDuration := time.Since(startTime)
	if err != nil {
		log.Error().Err(err).Dur("duration", cmdDuration).Msg("cmd failed")
	} else {
		log.Debug().Dur("duration", cmdDuration).Msg("cmd done")
	}

	if err := clxc.release(); err != nil {
		log.Error().Err(err).Msg("failed to release container")
	}
	if err != nil {
		// write diagnostics message to stderr for crio/kubelet
		println(err.Error())
		os.Exit(1)
	}
}

func setEnvIfNew(key, val string) error {
	if _, exist := os.LookupEnv(key); !exist {
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
