package main

import (
	"fmt"
	"os"

	"github.com/lxc/crio-lxc/cmd/internal"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
	"time"
)

var startCmd = cli.Command{
	Name:   "start",
	Usage:  "starts a container",
	Action: doStart,
	ArgsUsage: `[containerID]

starts <containerID>
`,
	Flags: []cli.Flag{
		&cli.DurationFlag{
			Name:        "timeout",
			Usage:       "timeout for reading from syncfifo",
			EnvVars:     []string{"CRIO_LXC_TIMEOUT_START"},
			Value:       time.Second * 30,
			Destination: &clxc.StartTimeout,
		},
	},
}

func doStart(ctx *cli.Context) error {
	fifoPath := clxc.runtimePath(internal.SyncFifoPath)
	// #nosec
	f, err := os.OpenFile(fifoPath, os.O_RDONLY, 0)
	log.Debug().Err(err).Str("fifo", fifoPath).Msg("open fifo")
	if err != nil {
		return errors.Wrap(err, "container not started - failed to open sync fifo")
	}
	// #nosec
	defer f.Close()

	done := make(chan error)

	go func() {
		data := make([]byte, len(internal.SyncFifoContent))
		n, err := f.Read(data)
		if err != nil {
			done <- errors.Wrapf(err, "problem reading from fifo")
		}
		if n != len(internal.SyncFifoContent) || string(data) != internal.SyncFifoContent {
			done <- errors.Errorf("bad fifo content: %s", string(data))
		}
		done <- nil
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(clxc.StartTimeout):
		return fmt.Errorf("timeout reading from syncfifo %s", fifoPath)
	}
}
