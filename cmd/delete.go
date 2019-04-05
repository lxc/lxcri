package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/urfave/cli"

	lxc "gopkg.in/lxc/go-lxc.v2"
)

var deleteCmd = cli.Command{
	Name:   "delete",
	Usage:  "deletes a container",
	Action: doDelete,
	ArgsUsage: `[containerID]

<containerID> is the ID of the container to delete
`,
	Flags: []cli.Flag{},
}

func doDelete(ctx *cli.Context) error {
	containerID := ctx.Args().Get(0)
	if len(containerID) == 0 {
		fmt.Fprintf(os.Stderr, "missing container ID\n")
		cli.ShowCommandHelpAndExit(ctx, "state", 1)
	}

	exists, err := containerExists(containerID)
	if err != nil {
		return errors.Wrap(err, "failed to check if container exists")
	}
	if !exists {
		return fmt.Errorf("container '%s' not found", containerID)
	}

	c, err := lxc.NewContainer(containerID, LXC_PATH)
	if err != nil {
		return errors.Wrap(err, "failed to load container")
	}
	defer c.Release()

	if err := configureLogging(ctx, c); err != nil {
		return errors.Wrap(err, "failed to configure logging")

	}

	if c.Running() {
		return fmt.Errorf("container '%s' is running, cannot delete.", containerID)
	}

	// TODO: lxc-destroy deletes the rootfs.
	// this appears to contradict the runtime spec:

	// "Note that resources associated with the container,
	// but not created by this container, MUST NOT be deleted.Note
	// that resources associated with the container, but not
	// created by this container, MUST NOT be deleted.

	if err := c.Destroy(); err != nil {
		return errors.Wrap(err, "failed to delete container.")
	}

	// TODO - because we set rootfs.managed=0, Destroy() doesn't
	// delete the /var/lib/lxc/$containerID/config file:
	configDir := filepath.Join(LXC_PATH, containerID)
	if err := os.RemoveAll(configDir); err != nil {
		return errors.Wrapf(err, "failed to remove %s", configDir)
	}

	return nil
}
