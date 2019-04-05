package main

import (
	"os"
	"os/exec"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

const (
	SYNC_FIFO_PATH    = "/syncfifo"
	SYNC_FIFO_CONTENT = "meshuggah rocks"
)

var fifoWaitCmd = cli.Command{
	Name:   "fifo-wait",
	Action: doFifoWait,
	Hidden: true,
}

func doFifoWait(ctx *cli.Context) error {
	syncPipe, err := os.Open(SYNC_FIFO_PATH)
	if err != nil {
		return errors.Wrapf(err, "couldn't open %s", SYNC_FIFO_PATH)
	}
	defer syncPipe.Close()

	fi, err := syncPipe.Stat()
	if err != nil {
		return errors.Wrapf(err, "couldn't stat %s", SYNC_FIFO_PATH)
	}

	if fi.Mode()&os.ModeNamedPipe == 0 {
		return errors.Errorf("%s exists and is not a named pipe", SYNC_FIFO_PATH)
	}

	_, err = syncPipe.WriteString(SYNC_FIFO_CONTENT)
	if err != nil {
		return errors.Wrapf(err, "could write to fifo")
	}

	cmd := exec.Command(ctx.Args().Get(0), ctx.Args().Tail()...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
