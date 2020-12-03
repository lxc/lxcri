package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/lxc/crio-lxc/lxcontainer"
	"github.com/urfave/cli/v2"
)

// Environment variables are populated by default from this environment file.
// Existing environment variables are preserved.
var envFile = "/etc/default/crio-lxc"

// The singelton that wraps the lxc.Container
var clxc struct {
	lxcontainer.Runtime

	Command    string
	CreateHook string

	CreateTimeout time.Duration
	StartTimeout  time.Duration
	KillTimeout   time.Duration
	DeleteTimeout time.Duration
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
			Destination: &clxc.LogLevel,
		},
		&cli.StringFlag{
			Name:        "container-log-level",
			Usage:       "set the container process (liblxc) log level (trace|debug|info|notice|warn|error|crit|alert|fatal)",
			EnvVars:     []string{"CRIO_LXC_CONTAINER_LOG_LEVEL"},
			Value:       "warn",
			Destination: &clxc.ContainerLogLevel,
		},
		&cli.StringFlag{
			Name:        "log-file",
			Usage:       "path to the log file for runtime and container output",
			EnvVars:     []string{"CRIO_LXC_LOG_FILE"},
			Value:       "/var/log/crio-lxc/crio-lxc.log",
			Destination: &clxc.LogFilePath,
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
			Destination: &clxc.InitCommand,
		},
		&cli.StringFlag{
			Name:        "cmd-start",
			Usage:       "absolute path to container start executable",
			EnvVars:     []string{"CRIO_LXC_START_CMD"},
			Value:       "/usr/local/bin/crio-lxc-start",
			Destination: &clxc.StartCommand,
		},
		&cli.StringFlag{
			Name:        "container-hook",
			Usage:       "absolute path to container hook executable",
			EnvVars:     []string{"CRIO_LXC_CONTAINER_HOOK"},
			Value:       "/usr/local/bin/crio-lxc-container-hook",
			Destination: &clxc.ContainerHook,
		},
		&cli.BoolFlag{
			Name:        "apparmor",
			Usage:       "set apparmor profile defined in container spec",
			Destination: &clxc.Apparmor,
			EnvVars:     []string{"CRIO_LXC_APPARMOR"},
			Value:       true,
		},
		&cli.BoolFlag{
			Name:        "capabilities",
			Usage:       "keep capabilities defined in container spec",
			Destination: &clxc.Capabilities,
			EnvVars:     []string{"CRIO_LXC_CAPABILITIES"},
			Value:       true,
		},
		&cli.BoolFlag{
			Name:        "cgroup-devices",
			Usage:       "allow only devices permitted by container spec",
			Destination: &clxc.CgroupDevices,
			EnvVars:     []string{"CRIO_LXC_CGROUP_DEVICES"},
			Value:       true,
		},
		&cli.BoolFlag{
			Name:        "seccomp",
			Usage:       "Generate and apply seccomp profile for lxc from container spec",
			Destination: &clxc.Seccomp,
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
				err = fmt.Errorf("failed to set environment variable \"%s=%s\": %w", err)
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
		clxc.Command = ctx.Args().Get(0)
		return nil
	}

	setupCmd := func(ctx *cli.Context) error {
		containerID := ctx.Args().Get(0)
		if len(containerID) == 0 {
			return fmt.Errorf("missing container ID")
		}
		clxc.ContainerID = containerID
		return clxc.ConfigureLogging(ctx.Command.Name)
	}

	for _, cmd := range app.Commands {
		cmd.Before = setupCmd
		cmd.OnUsageError = errUsage
	}

	err = app.Run(os.Args)

	cmdDuration := time.Since(startTime)

	if err != nil {
		clxc.Log.Error().Err(err).Dur("duration", cmdDuration).Msg("cmd failed")
		clxc.Release()
		// exit with exit status of executed command
		if err, yes := err.(execError); yes {
			os.Exit(err.ExitStatus())
		}
		// write diagnostics message to stderr for crio/kubelet
		println(err.Error())
		os.Exit(1)
	}

	clxc.Log.Debug().Dur("duration", cmdDuration).Msg("cmd completed")
	if clxc.Release(); err != nil {
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
			Destination: &clxc.BundlePath,
		},
		&cli.StringFlag{
			Name:        "console-socket",
			Usage:       "send container pty master fd to this socket path",
			Destination: &clxc.ConsoleSocket,
		},
		&cli.StringFlag{
			Name:        "pid-file",
			Usage:       "path to write container PID",
			Destination: &clxc.PidFile,
		},
		&cli.DurationFlag{
			Name:        "timeout",
			Usage:       "maximum duration for create to complete",
			EnvVars:     []string{"CRIO_LXC_CREATE_TIMEOUT"},
			Value:       time.Second * 60,
			Destination: &clxc.CreateTimeout,
		},
		&cli.StringFlag{
			Name:        "create-hook",
			Usage:       "absolute path to executable to run after create",
			EnvVars:     []string{"CRIO_LXC_CREATE_HOOK"},
			Destination: &clxc.CreateHook,
		},
	},
}

func doCreate(unused *cli.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), clxc.CreateTimeout)
	defer cancel()

	err := clxc.Create(ctx)

	if clxc.CreateHook != "" {
		env := []string{
			"CONTAINER_ID=" + clxc.ContainerID,
			"LXC_CONFIG=" + clxc.ConfigFilePath(),
			"RUNTIME_CMD=" + clxc.Command,
			"RUNTIME_PATH=" + clxc.RuntimePath(),
			"BUNDLE_PATH=" + clxc.BundlePath,
			"SPEC_PATH=" + clxc.SpecPath(),
			"LOG_FILE=" + clxc.LogFilePath,
		}
		if err != nil {
			env = append(env, "RUNTIME_ERROR="+err.Error())
		}
		cmd := exec.CommandContext(ctx, clxc.CreateHook)
		cmd.Env = env

		clxc.Log.Debug().Str("file", clxc.CreateHook).Msg("execute create hook")
		if err := cmd.Run(); err != nil {
			clxc.Log.Error().Err(err).Str("file", clxc.CreateHook).Msg("failed to execute create hook")
		} else {
			clxc.Log.Debug().Str("file", clxc.CreateHook).Msg("failed to execute runtime hook")
		}
	}
	return err
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
			Usage:       "timeout for reading from syncfifo",
			EnvVars:     []string{"CRIO_LXC_START_TIMEOUT"},
			Value:       time.Second * 30,
			Destination: &clxc.StartTimeout,
		},
	},
}

func doStart(unused *cli.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), clxc.StartTimeout)
	defer cancel()
	return clxc.Start(ctx)
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
	state, err := clxc.State()
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
			Destination: &clxc.KillTimeout,
		},
	},
}

func doKill(ctx *cli.Context) error {
	sig := ctx.Args().Get(1)
	signum := parseSignal(sig)
	if signum == 0 {
		return fmt.Errorf("invalid signal param %q", sig)
	}
	c, cancel := context.WithTimeout(context.Background(), clxc.KillTimeout)
	defer cancel()
	return clxc.Kill(c, signum)
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
			Destination: &clxc.DeleteTimeout,
		},
	},
}

func doDelete(ctx *cli.Context) error {
	c, cancel := context.WithTimeout(context.Background(), clxc.DeleteTimeout)
	defer cancel()
	return clxc.Delete(c, ctx.Bool("force"))
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

func (e execError) ExitStatus() int {
	return int(e)
}

func (e execError) Error() string {
	return fmt.Sprintf("exec cmd exited with status %d", int(e))
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

	procSpec, err := lxcontainer.ReadSpecProcess(ctx.String("process"))
	if err != nil {
		return err
	}
	if procSpec != nil {
		args = procSpec.Args
	}

	if detach {
		pid, err := clxc.ExecDetached(args, procSpec)
		if err != nil {
			return err
		}
		if pidFile != "" {
			return lxcontainer.CreatePidFile(pidFile, pid)
		}
	} else {
		status, err := clxc.Exec(args, procSpec)
		if err != nil {
			return err
		}
		if status != 0 {
			return execError(status)
		}
	}
	return nil
}
