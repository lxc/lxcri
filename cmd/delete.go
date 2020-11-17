package main

import (
	"fmt"
	"os"

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
	if err != nil {
		return err
	}

	if !clxc.isContainerStopped() {
		if !ctx.Bool("force") {
			return fmt.Errorf("container is not not stopped (current state %s)", clxc.Container.State())
		}
		if err := clxc.Container.Stop(); err != nil {
			return errors.Wrap(err, "failed to stop container")
		}
	}

	if err := clxc.Container.Destroy(); err != nil {
		return errors.Wrap(err, "failed to delete container")
	}

	tryRemoveCgroups(&clxc)

	// "Note that resources associated with the container,
	// but not created by this container, MUST NOT be deleted."
	// TODO - because we set rootfs.managed=0, Destroy() doesn't
	// delete the /var/lib/lxc/$containerID/config file:
	return os.RemoveAll(clxc.runtimePath())
}
