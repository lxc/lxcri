package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

var stateCmd = cli.Command{
	Name:   "state",
	Usage:  "returns state of a container",
	Action: doState,
	ArgsUsage: `[containerID]

<containerID> is the ID of the container you want to know about.
`,
	Flags: []cli.Flag{},
}

func doState(ctx *cli.Context) error {
	err := clxc.loadContainer()
	if err != nil {
		return errors.Wrapf(err, "failed to load container")
	}

	pid, err := clxc.readPidFile()
	if err != nil {
		return errors.Wrapf(err, "failed to load pidfile")
	}

	spec, err := clxc.readSpec()
	if err != nil {
		return err
	}

	s := specs.State{
		Version:     specs.Version,
		ID:          clxc.Container.Name(),
		Bundle:      clxc.BundlePath,
		Pid:         pid,
		Annotations: spec.Annotations,
	}

	//s.Annotations = spec.Annotations

	state, err := clxc.getContainerState()
	s.Status = string(state)
	if err != nil {
		return err
	}

	log.Info().Int("pid", s.Pid).Str("status", s.Status).Msg("container state")

	if stateJSON, err := json.Marshal(s); err == nil {
		fmt.Fprint(os.Stdout, string(stateJSON))
		log.Trace().RawJSON("state", stateJSON).Msg("container state")
	} else {
		return errors.Wrap(err, "failed to marshal json")
	}
	return err
}
