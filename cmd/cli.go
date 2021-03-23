package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/drachenfels-de/lxcri"
	"github.com/drachenfels-de/lxcri/log"
	"github.com/urfave/cli/v2"
)

// Environment variables are populated by default from this environment file.
// Existing environment variables are preserved.
var envFile = "/etc/default/crio-lxc"

const defaultLogFile = "/var/log/crio-lxc/crio-lxc.log"

type app struct {
	lxcri.Runtime
	containerConfig lxcri.ContainerConfig

	logConfig struct {
		File      *os.File
		FilePath  string
		Level     string
		Timestamp string
	}

	command           string
	createHook        string
	createHookTimeout time.Duration
}

var clxc = app{}

func (app *app) configureLogger() error {
	// TODO use console logger if filepath is /dev/stdout or /dev/stderr ?
	l, err := log.OpenFile(app.logConfig.FilePath, 0600)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	app.logConfig.File = l

	level, err := log.ParseLevel(app.logConfig.Level)
	if err != nil {
		return fmt.Errorf("failed to parse log level: %w", err)
	}
	logCtx := log.NewLogger(app.logConfig.File, level)
	app.Runtime.Log = logCtx.Str("cmd", app.command).Str("cid", app.containerConfig.ContainerID).Logger()
	app.containerConfig.Log = app.Runtime.Log

	return nil
}

func (app *app) release() error {
	if app.logConfig.File != nil {
		return app.logConfig.File.Close()
	}
	return nil
}

var version string

func main() {
	app := cli.NewApp()
	app.Name = "crio-lxc"
	app.Usage = "crio-lxc is a CRI compliant runtime wrapper for lxc"
	app.Version = version

	// Disable the default ExitErrHandler.
	// It will call os.Exit if a command returns an error that implements
	// the cli.ExitCoder interface. E.g an unwrapped error from os.Exec.
	app.ExitErrHandler = func(context *cli.Context, err error) {}
	app.Commands = []*cli.Command{
		&stateCmd,
		&createCmd,
		&startCmd,
		&killCmd,
		&deleteCmd,
		&execCmd,
		// TODO extend urfave/cli to render a default environment file.
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "log-level",
			Usage:       "set the runtime log level (trace|debug|info|warn|error)",
			EnvVars:     []string{"CRIO_LXC_LOG_LEVEL"},
			Value:       "info",
			Destination: &clxc.logConfig.Level,
		},
		&cli.StringFlag{
			Name:        "log-file",
			Usage:       "path to the log file for runtime and container output",
			EnvVars:     []string{"CRIO_LXC_LOG_FILE"},
			Value:       defaultLogFile,
			Destination: &clxc.logConfig.FilePath,
		},
		&cli.StringFlag{
			Name:        "log-timestamp",
			Usage:       "timestamp format for the runtime log (see golang time package), default matches liblxc timestamp",
			EnvVars:     []string{"CRIO_LXC_LOG_TIMESTAMP"}, // e.g  '0102 15:04:05.000'
			Destination: &clxc.logConfig.Timestamp,
		},
		&cli.StringFlag{
			Name:        "container-log-level",
			Usage:       "set the container process (liblxc) log level (trace|debug|info|notice|warn|error|crit|alert|fatal)",
			EnvVars:     []string{"CRIO_LXC_CONTAINER_LOG_LEVEL"},
			Value:       "warn",
			Destination: &clxc.containerConfig.LogLevel,
		},
		&cli.StringFlag{
			Name:        "container-log-file",
			Usage:       "path to the log file for runtime and container output",
			EnvVars:     []string{"CRIO_LXC_CONTAINER_LOG_FILE"},
			Value:       defaultLogFile,
			Destination: &clxc.containerConfig.LogFile,
		},
		&cli.StringFlag{
			Name:  "root",
			Usage: "container runtime root where (logs, init and hook scripts). tmpfs is recommended.",
			// exec permissions are not required because init is bind mounted into the root
			Value:       "/run/crio-lxc",
			Destination: &clxc.Root,
		},
		&cli.BoolFlag{
			Name:        "systemd-cgroup",
			Usage:       "enable systemd cgroup",
			Destination: &clxc.SystemdCgroup,
		},
		&cli.StringFlag{
			Name:        "monitor-cgroup",
			Usage:       "cgroup slice for liblxc monitor process and pivot path",
			Destination: &clxc.MonitorCgroup,
			EnvVars:     []string{"CRIO_LXC_MONITOR_CGROUP"},
			Value:       "crio-lxc-monitor.slice",
		},
		&cli.StringFlag{
			Name:        "cmd-init",
			Usage:       "absolute path to container init executable",
			EnvVars:     []string{"CRIO_LXC_INIT_CMD"},
			Value:       "/usr/local/bin/crio-lxc-init",
			Destination: &clxc.Executables.Init,
		},
		&cli.StringFlag{
			Name:        "cmd-start",
			Usage:       "absolute path to container start executable",
			EnvVars:     []string{"CRIO_LXC_START_CMD"},
			Value:       "/usr/local/bin/crio-lxc-start",
			Destination: &clxc.Executables.Start,
		},
		&cli.StringFlag{
			Name:        "container-hook",
			Usage:       "absolute path to container hook executable",
			EnvVars:     []string{"CRIO_LXC_CONTAINER_HOOK"},
			Value:       "/usr/local/bin/crio-lxc-container-hook",
			Destination: &clxc.Executables.Hook,
		},
		&cli.BoolFlag{
			Name:        "apparmor",
			Usage:       "set apparmor profile defined in container spec",
			Destination: &clxc.Features.Apparmor,
			EnvVars:     []string{"CRIO_LXC_APPARMOR"},
			Value:       true,
		},
		&cli.BoolFlag{
			Name:        "capabilities",
			Usage:       "keep capabilities defined in container spec",
			Destination: &clxc.Features.Capabilities,
			EnvVars:     []string{"CRIO_LXC_CAPABILITIES"},
			Value:       true,
		},
		&cli.BoolFlag{
			Name:        "cgroup-devices",
			Usage:       "allow only devices permitted by container spec",
			Destination: &clxc.Features.CgroupDevices,
			EnvVars:     []string{"CRIO_LXC_CGROUP_DEVICES"},
			Value:       true,
		},
		&cli.BoolFlag{
			Name:        "seccomp",
			Usage:       "Generate and apply seccomp profile for lxc from container spec",
			Destination: &clxc.Features.Seccomp,
			EnvVars:     []string{"CRIO_LXC_SECCOMP"},
			Value:       true,
		},
	}

	startTime := time.Now()

	// Environment variables must be injected from file before app.Run() is called.
	// Otherwise the values are not set to the crioLXC instance.
	// FIXME when calling '--help' defaults are overwritten with environment variables.
	// So you will never see the real default value if either an environment file is present
	// or an environment variable is set.
	env, err := loadEnvFile(envFile)
	if err != nil {
		println(err.Error())
		os.Exit(1)
	}
	if env != nil {
		for key, val := range env {
			if err := setEnv(key, val, false); err != nil {
				err = fmt.Errorf("failed to set environment variable \"%s=%s\": %w", key, val, err)
				println(err.Error())
				os.Exit(1)
			}
		}
	}

	app.CommandNotFound = func(ctx *cli.Context, cmd string) {
		fmt.Fprintf(os.Stderr, "undefined subcommand %q cmdline%s\n", cmd, os.Args)
	}
	// Disable the default error messages for cmdline errors.
	// By default the app/cmd help is printed to stdout, which produces garbage in cri-o log output.
	// Instead the cmdline is printed to stderr to identify cmdline interface errors.
	errUsage := func(context *cli.Context, err error, isSubcommand bool) error {
		fmt.Fprintf(os.Stderr, "usage error %s: %s\n", err, os.Args)
		return err
	}
	app.OnUsageError = errUsage

	app.Before = func(ctx *cli.Context) error {
		clxc.command = ctx.Args().Get(0)
		return nil
	}

	setupCmd := func(ctx *cli.Context) error {
		containerID := ctx.Args().Get(0)
		if len(containerID) == 0 {
			return fmt.Errorf("missing container ID")
		}
		clxc.containerConfig.ContainerID = containerID

		if err := clxc.configureLogger(); err != nil {
			return fmt.Errorf("failed to configure logger: %w", err)
		}
		return nil
	}

	for _, cmd := range app.Commands {
		cmd.Before = setupCmd
		cmd.OnUsageError = errUsage
	}

	err = app.Run(os.Args)

	cmdDuration := time.Since(startTime)

	if err != nil {
		clxc.Log.Error().Err(err).Dur("duration", cmdDuration).Msg("cmd failed")
		clxc.release()
		// write diagnostics message to stderr for crio/kubelet
		println(err.Error())

		// exit with exit status of executed command
		var errExec execError
		if errors.As(err, &errExec) {
			os.Exit(errExec.exitStatus())
		}
		os.Exit(1)
	}

	clxc.Log.Debug().Dur("duration", cmdDuration).Msg("cmd completed")
	if clxc.release(); err != nil {
		println(err.Error())
		os.Exit(1)
	}
}

var createCmd = cli.Command{
	Name:      "create",
	Usage:     "create a container from a bundle directory",
	ArgsUsage: "<containerID>",
	Action:    doCreate,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:        "bundle",
			Usage:       "set bundle directory",
			Value:       ".",
			Destination: &clxc.containerConfig.BundlePath,
		},
		&cli.StringFlag{
			Name:        "console-socket",
			Usage:       "send container pty master fd to this socket path",
			Destination: &clxc.containerConfig.ConsoleSocket,
		},
		&cli.StringFlag{
			Name:        "pid-file",
			Usage:       "path to write container PID",
			Destination: &clxc.containerConfig.PidFile,
		},
		&cli.DurationFlag{
			Name:        "timeout",
			Usage:       "maximum duration for create to complete",
			EnvVars:     []string{"CRIO_LXC_CREATE_TIMEOUT"},
			Value:       time.Second * 60,
			Destination: &clxc.Timeouts.Create,
		},
		// TODO implement OCI hooks and move to Runtime.Hooks
		&cli.StringFlag{
			Name:        "create-hook",
			Usage:       "absolute path to executable to run after create",
			EnvVars:     []string{"CRIO_LXC_CREATE_HOOK"},
			Destination: &clxc.createHook,
		},
		&cli.DurationFlag{
			Name:        "hook-timeout",
			Usage:       "maximum duration for hook to complete",
			EnvVars:     []string{"CRIO_LXC_CREATE_HOOK_TIMEOUT"},
			Value:       time.Second * 5,
			Destination: &clxc.createHookTimeout,
		},
	},
}

func doCreate(unused *cli.Context) error {
	if err := clxc.CheckSystem(); err != nil {
		return err
	}
	specPath := filepath.Join(clxc.containerConfig.BundlePath, "config.json")
	err := clxc.containerConfig.LoadSpecJson(specPath)
	if err != nil {
		return fmt.Errorf("failed to load container spec from bundle: %w", err)
	}
	c, err := clxc.Create(context.Background(), &clxc.containerConfig)
	if err == nil {
		defer c.Release()
	}
	runCreateHook(err)
	return err
}

func runCreateHook(err error) {
	env := []string{
		"CONTAINER_ID=" + clxc.containerConfig.ContainerID,
		"LXC_CONFIG=" + clxc.containerConfig.ConfigFilePath(),
		"RUNTIME_CMD=" + clxc.command,
		"RUNTIME_PATH=" + clxc.containerConfig.RuntimePath(),
		"BUNDLE_PATH=" + clxc.containerConfig.BundlePath,
		"SPEC_PATH=" + clxc.containerConfig.SpecPath,
		"LOG_FILE=" + clxc.logConfig.FilePath,
	}
	if err != nil {
		env = append(env, "RUNTIME_ERROR="+err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), clxc.createHookTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, clxc.createHook)
	cmd.Env = env

	clxc.Log.Debug().Str("file", clxc.createHook).Msg("execute create hook")
	if err := cmd.Run(); err != nil {
		clxc.Log.Error().Err(err).Str("file", clxc.createHook).Msg("failed to execute create hook")
	}
}

var startCmd = cli.Command{
	Name:   "start",
	Usage:  "starts a container",
	Action: doStart,
	ArgsUsage: `[containerID]

starts <containerID>
`,
	Flags: []cli.Flag{
		&cli.DurationFlag{
			Name:        "timeout",
			Usage:       "start timeout",
			EnvVars:     []string{"CRIO_LXC_START_TIMEOUT"},
			Value:       time.Second * 30,
			Destination: &clxc.Timeouts.Start,
		},
	},
}

func doStart(unused *cli.Context) error {
	c, err := clxc.Load(&clxc.containerConfig)
	if err != nil {
		return fmt.Errorf("failed to load container: %w", err)
	}

	return clxc.Start(context.Background(), c)
}

var stateCmd = cli.Command{
	Name:   "state",
	Usage:  "returns state of a container",
	Action: doState,
	ArgsUsage: `[containerID]

<containerID> is the ID of the container you want to know about.
`,
	Flags: []cli.Flag{},
}

func doState(unused *cli.Context) error {
	c, err := clxc.Load(&clxc.containerConfig)
	if err != nil {
		return fmt.Errorf("failed to load container: %w", err)
	}
	state, err := c.State()
	if err != nil {
		return err
	}
	j, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal json: %w", err)
	}
	clxc.Log.Trace().RawJSON("state", j).Msg("container state")
	_, err = fmt.Fprint(os.Stdout, string(j))
	return err
}

var killCmd = cli.Command{
	Name:   "kill",
	Usage:  "sends a signal to a container",
	Action: doKill,
	ArgsUsage: `[containerID] [signal]

<containerID> is the ID of the container to send a signal to
[signal] signal name or numerical value (e.g [9|kill|KILL|sigkill|SIGKILL])
`,
	Flags: []cli.Flag{
		&cli.DurationFlag{
			Name:        "timeout",
			Usage:       "timeout for killing all processes in container cgroup",
			EnvVars:     []string{"CRIO_LXC_KILL_TIMEOUT"},
			Value:       time.Second * 10,
			Destination: &clxc.Timeouts.Kill,
		},
	},
}

func doKill(ctx *cli.Context) error {
	sig := ctx.Args().Get(1)
	signum := parseSignal(sig)
	if signum == 0 {
		return fmt.Errorf("invalid signal param %q", sig)
	}

	c, err := clxc.Load(&clxc.containerConfig)
	if err != nil {
		return fmt.Errorf("failed to load container: %w", err)
	}
	return clxc.Kill(context.Background(), c, signum)
}

var deleteCmd = cli.Command{
	Name:   "delete",
	Usage:  "deletes a container",
	Action: doDelete,
	ArgsUsage: `[containerID]

<containerID> is the ID of the container to delete
`,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "force",
			Usage: "force deletion",
		},
		&cli.DurationFlag{
			Name:        "timeout",
			Usage:       "timeout for deleting container",
			EnvVars:     []string{"CRIO_LXC_DELETE_TIMEOUT"},
			Value:       time.Second * 10,
			Destination: &clxc.Timeouts.Delete,
		},
	},
}

func doDelete(ctx *cli.Context) error {
	c, err := clxc.Load(&clxc.containerConfig)
	if err == lxcri.ErrNotExist {
		clxc.Log.Info().Msg("container does not exist")
		return nil
	}

	return clxc.Delete(context.Background(), c, ctx.Bool("force"))
}

var execCmd = cli.Command{
	Name:      "exec",
	Usage:     "execute a new process in a running container",
	ArgsUsage: "<containerID>",
	Action:    doExec,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "process",
			Aliases: []string{"p"},
			Usage:   "path to process json",
			Value:   "",
		},
		&cli.StringFlag{
			Name:  "pid-file",
			Usage: "file to write the process id to",
			Value: "",
		},
		&cli.BoolFlag{
			Name:    "detach",
			Aliases: []string{"d"},
			Usage:   "detach from the executed process",
		},
	},
}

type execError int

func (e execError) exitStatus() int {
	return int(e)
}

func (e execError) Error() string {
	// liblxc remaps execvp exit codes to shell exit codes.
	// FIXME This is undocumented behaviour lxc/src/lxc/attach.c:lxc_attach_run_command
	// https://github.com/lxc/go-lxc/blob/d1943fb48dc73ef5cbc0ef43ed585420f7b2eb3a/container.go#L1370
	// RunCommandStatus returns with exitCode 126 or 127 but without error, so it is not possible to determine
	// whether this is the exit code from the command itself (e.g a shell itself) or from liblxc exec.
	switch int(e) {
	case 126:
		return "can not execute file: file header not recognized"
	case 127:
		return "executable file not found in $PATH"
	default:
		return fmt.Sprintf("cmd execution failed with exit status %d", e.exitStatus())
	}
}

func doExec(ctx *cli.Context) error {
	var args []string
	if ctx.Args().Len() > 1 {
		args = ctx.Args().Slice()[1:]
	}

	pidFile := ctx.String("pid-file")
	detach := ctx.Bool("detach")

	if detach && pidFile == "" {
		clxc.Log.Warn().Msg("detaching process but pid-file value is unset")
	}

	procSpec, err := lxcri.ReadSpecProcessJSON(ctx.String("process"))
	if err != nil {
		return err
	}
	if procSpec != nil {
		args = procSpec.Args
	}

	c, err := clxc.Load(&clxc.containerConfig)
	if err != nil {
		return err
	}

	if detach {
		pid, err := c.ExecDetached(args, procSpec)
		if err != nil {
			return err
		}
		if pidFile != "" {
			return lxcri.CreatePidFile(pidFile, pid)
		}
	} else {
		status, err := c.Exec(args, procSpec)
		if err != nil {
			return err
		}
		if status != 0 {
			return execError(status)
		}
	}
	return nil
}
