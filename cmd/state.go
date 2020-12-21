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
	// TODO create a symlink to the pidfile and config in the runtime path
	bundlePath := filepath.Join("/var/run/containers/storage/overlay-containers/", clxc.Container.Name(), "userdata")
	pidFilePath := filepath.Join(bundlePath, "pidfile")

	pid, err := readPidFile(pidFilePath)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "failed to load pidfile")
	}

	spec, err := internal.ReadSpec(filepath.Join(bundlePath, "config.json"))
	if err != nil {
		return errors.Wrapf(err, "failed to load spec from %s", bundlePath)
	}

	s := specs.State{
		Version: specs.Version,
		ID:      clxc.Container.Name(),
		Bundle:  bundlePath,
		Pid:     pid,
	}

	if spec.Annotations != nil {
		s.Annotations = spec.Annotations
	}

	// FIXME do not return the init pid, but the container process PID instead ...
	_, status, err := clxc.getContainerState()
	s.Status = status

	log.Info().Int("pid", s.Pid).Str("status", s.Status).Msg("container state")

	if stateJSON, err := json.Marshal(s); err == nil {
		fmt.Fprint(os.Stdout, string(stateJSON))
		log.Trace().RawJSON("state", stateJSON).Msg("container state")
	} else {
		return errors.Wrap(err, "failed to marshal json")
	}
	return err
}
