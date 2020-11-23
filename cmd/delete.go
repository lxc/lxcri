package main

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

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
	},
}

func doDelete(ctx *cli.Context) error {

	err := clxc.loadContainer()
	if err == errContainerNotExist {
		return clxc.destroy()
	}
	if err != nil {
		return err
	}

	force := ctx.Bool("force")
	log.Info().Bool("force", force).Msg("delete container")

	if !clxc.isContainerStopped() {
		if !force {
			return fmt.Errorf("container is not not stopped (current state %s)", clxc.Container.State())
		}
		if err := clxc.Container.Stop(); err != nil {
			return errors.Wrap(err, "failed to stop container")
		}
	}
	return clxc.destroy()
}
