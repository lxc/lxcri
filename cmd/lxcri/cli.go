package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/drachenfels-de/lxcri"
	"github.com/drachenfels-de/lxcri/log"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/urfave/cli/v2"
)

var (
	// Environment variables are populated by default from this environment file.
	// Existing environment variables are preserved.
	envFile        = "/etc/default/lxcri"
	defaultLogFile = "/var/log/lxcri/lxcri.log"
	version        = "undefined"
	libexecDir     = "/usr/libexec/lxcri"
)

type app struct {
	lxcri.Runtime
	cfg lxcri.ContainerConfig

	logConfig struct {
		File      *os.File
		FilePath  string
		Level     string
		Timestamp string
	}

	command    string
	createHook string
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
	app.Runtime.Log = logCtx.Str("cmd", app.command).Str("cid", app.cfg.ContainerID).Logger()
	app.cfg.Log = app.Runtime.Log

	return nil
}

func (app *app) release() error {
	if app.logConfig.File != nil {
		return app.logConfig.File.Close()
	}
	return nil
}

func main() {
	app := cli.NewApp()
	app.Name = "lxcri"
	app.Usage = "lxcri is a CRI compliant runtime wrapper for lxc"
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
		&inspectCmd,
		// TODO extend urfave/cli to render a default environment file.
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "log-level",
			Usage:       "set the runtime log level (trace|debug|info|warn|error)",
			EnvVars:     []string{"LXCRI_LOG_LEVEL"},
			Value:       "info",
			Destination: &clxc.logConfig.Level,
		},
		&cli.StringFlag{
			Name:        "log-file",
			Usage:       "path to the log file for runtime and container output",
			EnvVars:     []string{"LXCRI_LOG_FILE"},
			Value:       defaultLogFile,
			Destination: &clxc.logConfig.FilePath,
		},
		&cli.StringFlag{
			Name:        "log-timestamp",
			Usage:       "timestamp format for the runtime log (see golang time package), default matches liblxc timestamp",
			EnvVars:     []string{"LXCRI_LOG_TIMESTAMP"}, // e.g  '0102 15:04:05.000'
			Destination: &clxc.logConfig.Timestamp,
		},
		&cli.StringFlag{
			Name:        "container-log-level",
			Usage:       "set the container process (liblxc) log level (trace|debug|info|notice|warn|error|crit|alert|fatal)",
			EnvVars:     []string{"LXCRI_CONTAINER_LOG_LEVEL"},
			Value:       "warn",
			Destination: &clxc.cfg.LogLevel,
		},
		&cli.StringFlag{
			Name:        "container-log-file",
			Usage:       "path to the log file for runtime and container output",
			EnvVars:     []string{"LXCRI_CONTAINER_LOG_FILE"},
			Value:       defaultLogFile,
			Destination: &clxc.cfg.LogFile,
		},
		&cli.StringFlag{
			Name:  "root",
			Usage: "container runtime root where (logs, init and hook scripts). tmpfs is recommended.",
			// exec permissions are not required because init is bind mounted into the root
			Value:       "/run/lxcri",
			Destination: &clxc.Root,
		},
		&cli.BoolFlag{
			Name:        "systemd-cgroup",
			Usage:       "enable support for systemd encoded cgroup path",
			Destination: &clxc.SystemdCgroup,
		},
		&cli.StringFlag{
			Name:        "monitor-cgroup",
			Usage:       "cgroup slice for liblxc monitor process and pivot path",
			Destination: &clxc.MonitorCgroup,
			EnvVars:     []string{"LXCRI_MONITOR_CGROUP"},
			Value:       "lxcri-monitor.slice",
		},
		&cli.StringFlag{
			Name:        "libexec",
			Usage:       "directory to load runtime executables from",
			EnvVars:     []string{"LXCRI_LIBEXEC"},
			Value:       libexecDir,
			Destination: &clxc.LibexecDir,
		},
		&cli.BoolFlag{
			Name:        "apparmor",
			Usage:       "set apparmor profile defined in container spec",
			Destination: &clxc.Features.Apparmor,
			EnvVars:     []string{"LXCRI_APPARMOR"},
			Value:       true,
		},
		&cli.BoolFlag{
			Name:        "capabilities",
			Usage:       "keep capabilities defined in container spec",
			Destination: &clxc.Features.Capabilities,
			EnvVars:     []string{"LXCRI_CAPABILITIES"},
			Value:       true,
		},
		&cli.BoolFlag{
			Name:        "cgroup-devices",
			Usage:       "allow only devices permitted by container spec",
			Destination: &clxc.Features.CgroupDevices,
			EnvVars:     []string{"LXCRI_CGROUP_DEVICES"},
			Value:       true,
		},
		&cli.BoolFlag{
			Name:        "seccomp",
			Usage:       "Generate and apply seccomp profile for lxc from container spec",
			Destination: &clxc.Features.Seccomp,
			EnvVars:     []string{"LXCRI_SECCOMP"},
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
		clxc.cfg.ContainerID = containerID

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
			Destination: &clxc.cfg.BundlePath,
		},
		&cli.StringFlag{
			Name:        "console-socket",
			Usage:       "send container pty master fd to this socket path",
			Destination: &clxc.cfg.ConsoleSocket,
		},
		&cli.StringFlag{
			Name:  "pid-file",
			Usage: "path to write container PID",
		},
		&cli.UintFlag{
			Name:    "timeout",
			Usage:   "maximum duration in seconds for create to complete",
			EnvVars: []string{"LXCRI_CREATE_TIMEOUT"},
			Value:   60,
		},
	},
}

func doCreate(ctxcli *cli.Context) error {
	if err := clxc.Init(); err != nil {
		return err
	}
	specPath := filepath.Join(clxc.cfg.BundlePath, lxcri.BundleConfigFile)
	spec, err := lxcri.ReadSpecJSON(specPath)
	if err != nil {
		return fmt.Errorf("failed to load container spec from bundle: %w", err)
	}
	clxc.cfg.Spec = spec
	pidFile := ctxcli.String("pid-file")

	timeout := time.Duration(ctxcli.Uint("timeout")) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c, err := clxc.Create(ctx, &clxc.cfg)
	if err != nil {
		return err
	}
	defer c.Release()
	if pidFile != "" {
		return createPidFile(pidFile, c.Pid)
	}
	return nil
}

var startCmd = cli.Command{
	Name:   "start",
	Usage:  "starts a container",
	Action: doStart,
	ArgsUsage: `[containerID]

starts <containerID>
`,
	Flags: []cli.Flag{
		&cli.UintFlag{
			Name:    "timeout",
			Usage:   "maximum duration in seconds for start to complete",
			EnvVars: []string{"LXCRI_START_TIMEOUT"},
			Value:   30,
		},
	},
}

func doStart(ctxcli *cli.Context) error {
	c, err := clxc.Load(clxc.cfg.ContainerID)
	if err != nil {
		return fmt.Errorf("failed to load container: %w", err)
	}

	timeout := time.Duration(ctxcli.Uint("timeout")) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return clxc.Start(ctx, c)
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
	c, err := clxc.Load(clxc.cfg.ContainerID)
	if err != nil {
		return fmt.Errorf("failed to load container: %w", err)
	}
	state, err := c.State()
	if err != nil {
		return err
	}
	j, err := json.Marshal(state.SpecState)
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
		&cli.UintFlag{
			Name:    "timeout",
			Usage:   "timeout for killing all processes in container cgroup",
			EnvVars: []string{"LXCRI_KILL_TIMEOUT"},
			Value:   10,
		},
	},
}

func doKill(ctxcli *cli.Context) error {
	sig := ctxcli.Args().Get(1)
	signum := parseSignal(sig)
	if signum == 0 {
		return fmt.Errorf("invalid signal param %q", sig)
	}

	c, err := clxc.Load(clxc.cfg.ContainerID)
	if err != nil {
		return fmt.Errorf("failed to load container: %w", err)
	}

	timeout := time.Duration(ctxcli.Uint("timeout")) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return clxc.Kill(ctx, c, signum)
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
			Name:    "timeout",
			Usage:   "maximum duration in seconds for delete to complete",
			EnvVars: []string{"LXCRI_DELETE_TIMEOUT"},
			Value:   10,
		},
	},
}

func doDelete(ctxcli *cli.Context) error {
	timeout := time.Duration(ctxcli.Uint("timeout")) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return clxc.Delete(ctx, clxc.cfg.ContainerID, ctxcli.Bool("force"))
}

var execCmd = cli.Command{
	Name:      "exec",
	Usage:     "execute a new process in a running container",
	ArgsUsage: "<containerID> [COMMAND] [args...]",
	Action:    doExec,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "process",
			Aliases: []string{"p"},
			Usage:   "path to process json - cmd and args are ignored if set",
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

func doExec(ctxcli *cli.Context) error {
	var args []string
	if ctxcli.Args().Len() > 1 {
		args = ctxcli.Args().Slice()[1:]
	}

	pidFile := ctxcli.String("pid-file")
	detach := ctxcli.Bool("detach")

	if detach && pidFile == "" {
		clxc.Log.Warn().Msg("detaching process but pid-file value is unset")
	}

	procSpec, err := lxcri.LoadSpecProcess(ctxcli.String("process"), args)
	if err != nil {
		return err
	}

	c, err := clxc.Load(clxc.cfg.ContainerID)
	if err != nil {
		return err
	}

	if detach {
		pid, err := c.ExecDetached(procSpec)
		if err != nil {
			return err
		}
		if pidFile != "" {
			return createPidFile(pidFile, pid)
		}
	} else {
		status, err := c.Exec(procSpec)
		if err != nil {
			return err
		}
		if status != 0 {
			return execError(status)
		}
	}
	return nil
}

var inspectCmd = cli.Command{
	Name:   "inspect",
	Usage:  "returns inspect of a container",
	Action: doInspect,
	ArgsUsage: `containerID [containerID...]

<containerID> [containerID...] list of IDs for container to inspect
`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "template",
			Usage: "Use this go template to to format output.",
		},
	},
}

func doInspect(ctxcli *cli.Context) (err error) {
	var t *template.Template
	tmpl := ctxcli.String("template")
	if tmpl != "" {
		t, err = template.New("inspect").Parse(tmpl)
		if err != nil {
			return err
		}
	}

	for _, id := range ctxcli.Args().Slice() {
		if err := inspectContainer(id, t); err != nil {
			return err
		}
	}
	return nil
}

func inspectContainer(id string, t *template.Template) error {
	c, err := clxc.Load(id)
	if err != nil {
		return fmt.Errorf("failed to load container: %w", err)
	}
	state, err := c.State()
	if err != nil {
		return fmt.Errorf("failed ot get container state: %w", err)
	}

	info := struct {
		Spec      *specs.Spec
		Container *lxcri.Container
		State     *lxcri.State
	}{
		Spec:      c.Spec,
		Container: c,
		State:     state,
	}

	if t != nil {
		return t.Execute(os.Stdout, info)
	}

	// avoid duplicate output
	c.Spec = nil
	state.SpecState.Annotations = nil

	j, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal json: %w", err)
	}
	_, err = fmt.Fprint(os.Stdout, string(j))
	return err
}
