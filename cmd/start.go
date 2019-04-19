package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/apex/log"
	//	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"github.com/urfave/cli"

	lxc "gopkg.in/lxc/go-lxc.v2"
)

var startCmd = cli.Command{
	Name:   "start",
	Usage:  "starts a container",
	Action: doStart,
	ArgsUsage: `[containerID]

starts <containerID>
`,
}

func checkHackyPreStart(c *lxc.Container) string {
	hooks := c.ConfigItem("lxc.hook.pre-start")
	for _, h := range hooks {
		if h == "/bin/true" {
			return "started"
		}
	}
	return "prestart"
}

func setHackyPreStart(c *lxc.Container) {
	err := c.SetConfigItem("lxc.hook.pre-start", "/bin/true")
	if err != nil {
		log.Warnf("Failed to set \"container started\" indicator: %v", err)
	}
	err = c.SaveConfigFile(filepath.Join(LXC_PATH, c.Name(), "config"))
	if err != nil {
		log.Warnf("Failed to save \"container started\" indicator: %v", err)
	}
}

func doStart(ctx *cli.Context) error {
	containerID := ctx.Args().Get(0)
	if len(containerID) == 0 {
		fmt.Fprintf(os.Stderr, "missing container ID\n")
		cli.ShowCommandHelpAndExit(ctx, "state", 1)
	}

	log.Infof("about to create container")
	c, err := lxc.NewContainer(containerID, LXC_PATH)
	if err != nil {
		return errors.Wrap(err, "failed to load container")
	}
	defer c.Release()
	log.Infof("checking if running")
	if !c.Running() {
		return fmt.Errorf("'%s' is not ready", containerID)
	}
	if checkHackyPreStart(c) == "started" {
		return fmt.Errorf("'%s' already running", containerID)
	}
	log.Infof("not running, can start")
	setHackyPreStart(c)
	fifoPath := filepath.Join(LXC_PATH, containerID, "syncfifo")
	log.Infof("opening fifo '%s'", fifoPath)
	f, err := os.OpenFile(fifoPath, os.O_RDWR, 0)
	if err != nil {
		return errors.Wrap(err, "failed to open sync fifo")
	}

	log.Infof("opened fifo, reading")
	data := make([]byte, len(SYNC_FIFO_CONTENT))
	n, err := f.Read(data)
	if err != nil {
		return errors.Wrapf(err, "problem reading from fifo")
	}
	if n != len(SYNC_FIFO_CONTENT) || string(data) != SYNC_FIFO_CONTENT {
		return errors.Errorf("bad fifo content: %s", string(data))
	}

	log.Infof("read '%s' from fifo, done", data)
	return nil
}
