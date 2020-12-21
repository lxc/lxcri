package main

import (
	"fmt"
	"os"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
	"golang.org/x/sys/unix"
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

	force := ctx.Bool("force")
	log.Info().Bool("force", force).Msg("delete container")

	if !clxc.isContainerStopped() {
		if !force {
			return fmt.Errorf("container is not not stopped (current state %s)", clxc.Container.State())
		}

		pid := clxc.Container.InitPid()
		if pid > 0 {
			log.Info().Int("pid", pid).Int("signal", 9).Msg("kill init")
			err := unix.Kill(pid, 9)
			if err != nil {
				return err
			}
		}
		// wait for container to be stopped ?
		//if ! clxc.Container.Wait(lxc.STOPPED, time.Second*30) {
		//  return fmt.Errorf("timeout")
		// }
	}

	if err := clxc.Container.Destroy(); err != nil {
		return errors.Wrap(err, "failed to destroy container")
	}

	//tryRemoveCgroups(&clxc)

	// "Note that resources associated with the container,
	// but not created by this container, MUST NOT be deleted."
	// TODO - because we set rootfs.managed=0, Destroy() doesn't
	// delete the /var/lib/lxc/$containerID/config file:

	return os.RemoveAll(clxc.runtimePath())
}
