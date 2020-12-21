package main

import (
	"time"

	"github.com/lxc/crio-lxc/cmd/internal"
	"github.com/urfave/cli/v2"
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
			Value:       time.Second * 60,
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

	return internal.ReadFifo(clxc.runtimePath(internal.SyncFifoPath), clxc.StartTimeout)
}
