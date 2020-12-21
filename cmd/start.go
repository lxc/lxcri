package main

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
	"os"
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
			EnvVars:     []string{"CRIO_LXC_START_TIMEOUT"},
			Value:       time.Second * 5,
			Destination: &clxc.StartTimeout,
		},
	},
}

func doStart(ctx *cli.Context) error {
	log.Info().Msg("notify init to start container process")

	err := clxc.loadContainer()
	if err != nil {
		return err
	}

	state, err := clxc.getContainerState()
	if err != nil {
		return err
	}
	if state != StateCreated {
		return fmt.Errorf("invalid container state. expected %q, but was %q", StateCreated, state)
	}

	done := make(chan error)
	go func() {
		done <- readFifo()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(clxc.StartTimeout):
		return fmt.Errorf("timeout reading from syncfifo")
	}

	return clxc.waitRunning(time.Second * 5)
}

func readFifo() error {
	// #nosec
	f, err := os.OpenFile(syncFifoPath(), os.O_RDONLY, 0)
	if err != nil {
		return errors.Wrap(err, "failed to open sync fifo")
	}
	// can not set deadline on fifo
	// #nosec
	defer f.Close()

	data := make([]byte, len(clxc.ContainerID))
	_, err = f.Read(data)
	if err != nil {
		return errors.Wrap(err, "problem reading from fifo")
	}
	if clxc.ContainerID != string(data) {
		return errors.Errorf("bad fifo content: %s", string(data))
	}
	return nil
}
