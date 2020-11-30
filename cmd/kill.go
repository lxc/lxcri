package main

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

var killCmd = cli.Command{
	Name:   "kill",
	Usage:  "sends a signal to a container",
	Action: doKill,
	ArgsUsage: `[containerID] [signal]

<containerID> is the ID of the container to send a signal to
[signal] signal name or numerical value (e.g [9|kill|KILL|sigkill|SIGKILL])
`,
}

func parseSignal(sig string) unix.Signal {
	if sig == "" {
		return unix.SIGTERM
	}
	// handle numerical signal value
	if num, err := strconv.Atoi(sig); err == nil {
		return unix.Signal(num)
	}

	// gracefully handle all string variants e.g 'sigkill|SIGKILL|kill|KILL'
	s := strings.ToUpper(sig)
	if !strings.HasPrefix(s, "SIG") {
		s = "SIG" + s
	}
	return unix.SignalNum(s)
}

func doKill(ctx *cli.Context) error {
	sig := ctx.Args().Get(1)
	signum := parseSignal(sig)
	if signum == 0 {
		return fmt.Errorf("invalid signal param %q", sig)
	}

	err := clxc.loadContainer()
	if err != nil {
		return errors.Wrap(err, "failed to load container")
	}

	state, err := clxc.getContainerState()
	if err != nil {
		return err
	}
	if !(state == StateCreated || state == StateRunning) {
		return fmt.Errorf("can only kill container in state Created|Running but was %q", state)
	}

	return clxc.killContainer(signum)
}
