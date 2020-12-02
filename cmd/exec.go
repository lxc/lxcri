package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"

	lxc "gopkg.in/lxc/go-lxc.v2"
)

type execError int

func (e execError) ExitStatus() int {
	return int(e)
}

func (e execError) Error() string {
	return fmt.Sprintf("exec cmd exited with status %d", int(e))
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
	spec, err := clxc.readSpec()
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

	attachOpts.StdinFd = 0
	attachOpts.StdoutFd = 1
	attachOpts.StderrFd = 2

	detach := ctx.Bool("detach")

	log.Info().Bool("detach", detach).Strs("args", procArgs).
		Int("uid", attachOpts.UID).Int("gid", attachOpts.GID).
		Ints("groups", attachOpts.Groups).Msg("running cmd in container")

	if detach {
		pidFile := ctx.String("pid-file")
		pid, err := c.RunCommandNoWait(procArgs, attachOpts)
		if err != nil {
			return errors.Wrapf(err, "c.RunCommandNoWait failed")
		}
		log.Debug().Err(err).Int("pid", pid).Msg("cmd is running detached")
		if pidFile == "" {
			log.Warn().Msg("detaching process but pid-file value is empty")
			return nil
		}
		return createPidFile(pidFile, pid)
	}

	exitStatus, err := c.RunCommandStatus(procArgs, attachOpts)
	if err != nil {
		return err
	}
	if exitStatus != 0 {
		return execError(exitStatus)
	}
	return nil
}
