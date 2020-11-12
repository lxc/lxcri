package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"

	"github.com/lxc/crio-lxc/cmd/internal"
	lxc "gopkg.in/lxc/go-lxc.v2"
)

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

func doExec(ctx *cli.Context) error {
	err := clxc.loadContainer()
	if err != nil {
		return errors.Wrap(err, "failed to load container")
	}
	c := clxc.Container

	attachOpts := lxc.AttachOptions{}

	var procArgs []string
	specFilePath := ctx.String("process")

	if specFilePath != "" {
		log.Debug().Str("spec", specFilePath).Msg("read process spec")
		// #nosec
		specData, err := ioutil.ReadFile(specFilePath)
		log.Trace().Err(err).RawJSON("spec", specData).Msg("process spec data")

		if err != nil {
			return errors.Wrap(err, "failed to read process spec")
		}

		var procSpec *specs.Process
		err = json.Unmarshal(specData, &procSpec)
		if err != nil {
			return errors.Wrapf(err, "failed to unmarshal process spec")
		}
		procArgs = procSpec.Args
		attachOpts.UID = int(procSpec.User.UID)
		attachOpts.GID = int(procSpec.User.GID)
		if n := len(procSpec.User.AdditionalGids); n > 0 {
			attachOpts.Groups = make([]int, n)
			for i, g := range procSpec.User.AdditionalGids {
				attachOpts.Groups[i] = int(g)
			}
		}
		log.Debug().Int("uid", attachOpts.UID).Int("gid", attachOpts.GID).Ints("groups", attachOpts.Groups).Msg("process user")
		log.Debug().Strs("arg", procArgs).Msg("process args")
		attachOpts.Cwd = procSpec.Cwd
		// Use the environment defined by the process spec.
		attachOpts.ClearEnv = true
		attachOpts.Env = procSpec.Env

	} else {
		// Fall back to cmdline arguments.
		if ctx.Args().Len() >= 2 {
			procArgs = ctx.Args().Slice()[1:]
		}
	}

	// Load container spec to get the list of supported namespaces.
	spec, err := internal.ReadSpec(clxc.runtimePath(internal.InitSpec))
	if err != nil {
		return errors.Wrap(err, "failed to read container runtime spec")
	}
	for _, ns := range spec.Linux.Namespaces {
		n, supported := namespaceMap[ns.Type]
		if !supported {
			return fmt.Errorf("can not attach to %s: unsupported namespace", ns.Type)
		}
		attachOpts.Namespaces |= n.CloneFlag
	}

	attachOpts.StdinFd = os.Stdin.Fd()
	attachOpts.StdoutFd = os.Stdout.Fd()
	attachOpts.StderrFd = os.Stderr.Fd()

	detach := ctx.Bool("detach")
	log.Debug().Bool("detach", detach).Strs("args", procArgs).Msg("exec cmd")

	if detach {
		pidFile := ctx.String("pid-file")
		pid, err := c.RunCommandNoWait(procArgs, attachOpts)
		if err != nil {
			return errors.Wrapf(err, "c.RunCommandNoWait failed")
		}
		log.Debug().Err(err).Int("pid", pid).Msg("cmd executed detached")
		if pidFile == "" {
			log.Warn().Msg("detaching process but pid-file value is empty")
			return nil
		}
		return createPidFile(pidFile, pid)
	}

	exitStatus, err := c.RunCommandStatus(procArgs, attachOpts)
	if err != nil {
		return errors.Wrapf(err, "c.RunCommandStatus returned with exit code %d", exitStatus)
	}
	log.Debug().Int("exit", exitStatus).Msg("cmd executed synchronous")

	return nil
}
