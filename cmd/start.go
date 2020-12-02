package main

import (
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
			Name:    "timeout",
			Usage:   "timeout for reading from syncfifo",
			EnvVars: []string{"CRIO_LXC_START_TIMEOUT"},
			Value:   time.Second * 5,
		},
	},
}

func doStart(ctx *cli.Context) error {
	return clxc.Start(ctx.Duration("timeout"))
}
