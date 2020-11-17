package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lxc/crio-lxc/cmd/internal"
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

	// TODO save BundlePath to init spec
	bundlePath := filepath.Join("/var/run/containers/storage/overlay-containers/", clxc.Container.Name(), "userdata")
	spec, err := internal.ReadSpec(filepath.Join(bundlePath, "config.json"))
	if err != nil {
		return errors.Wrapf(err, "failed to load spec from %s", bundlePath)
	}

	s := specs.State{
		Version: specs.Version,
		ID:      clxc.Container.Name(),
		Bundle:  bundlePath,
	}

	if spec.Annotations != nil {
		s.Annotations = spec.Annotations
	}

	s.Pid, s.Status, err = clxc.getContainerState()
	log.Info().Int("pid", s.Pid).Str("status", s.Status).Msg("container state")

	if stateJSON, err := json.Marshal(s); err == nil {
		fmt.Fprint(os.Stdout, string(stateJSON))
		log.Trace().RawJSON("state", stateJSON).Msg("container state")
	} else {
		return errors.Wrap(err, "failed to marshal json")
	}
	return err
}
