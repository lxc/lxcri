// +build go1.10

package main

import (
	"fmt"
	"os"
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

const sigzero = unix.Signal(0)

var signalMap = map[string]unix.Signal{
	"ABRT":   unix.SIGABRT,
	"ALRM":   unix.SIGALRM,
	"BUS":    unix.SIGBUS,
	"CHLD":   unix.SIGCHLD,
	"CLD":    unix.SIGCLD,
	"CONT":   unix.SIGCONT,
	"FPE":    unix.SIGFPE,
	"HUP":    unix.SIGHUP,
	"ILL":    unix.SIGILL,
	"INT":    unix.SIGINT,
	"IO":     unix.SIGIO,
	"IOT":    unix.SIGIOT,
	"KILL":   unix.SIGKILL,
	"PIPE":   unix.SIGPIPE,
	"POLL":   unix.SIGPOLL,
	"PROF":   unix.SIGPROF,
	"PWR":    unix.SIGPWR,
	"QUIT":   unix.SIGQUIT,
	"SEGV":   unix.SIGSEGV,
	"STKFLT": unix.SIGSTKFLT,
	"STOP":   unix.SIGSTOP,
	"SYS":    unix.SIGSYS,
	"TERM":   unix.SIGTERM,
	"TRAP":   unix.SIGTRAP,
	"TSTP":   unix.SIGTSTP,
	"TTIN":   unix.SIGTTIN,
	"TTOU":   unix.SIGTTOU,
	"URG":    unix.SIGURG,
	"USR1":   unix.SIGUSR1,
	"USR2":   unix.SIGUSR2,
	"VTALRM": unix.SIGVTALRM,
	"WINCH":  unix.SIGWINCH,
	"XCPU":   unix.SIGXCPU,
	"XFSZ":   unix.SIGXFSZ,
}

// Retrieve the PID from container init process safely.
func getSignal(ctx *cli.Context) (unix.Signal, error) {
	sig := ctx.Args().Get(1)
	if len(sig) == 0 {
		return sigzero, errors.New("missing signal")
	}

	// handle numerical signal value
	if num, err := strconv.Atoi(sig); err == nil {
		for _, signum := range signalMap {
			if num == int(signum) {
				return signum, nil
			}
		}
		return sigzero, fmt.Errorf("signal %s is not supported", sig)
	}

	// gracefully handle all string variants e.g 'sigkill|SIGKILL|kill|KILL'
	s := strings.TrimPrefix("SIG", strings.ToUpper(sig))
	signum, exists := signalMap[s]
	if !exists {
		return unix.Signal(0), fmt.Errorf("signal %s not supported", sig)
	}
	return signum, nil
}

func doKill(ctx *cli.Context) error {
	signum, err := getSignal(ctx)
	if err != nil {
		return errors.Wrap(err, "invalid signal param")
	}

	err = clxc.loadContainer()
	if err != nil {
		return errors.Wrap(err, "failed to load container")
	}

	pid, err := clxc.readPidFile()
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "failed to load pidfile")
	}
	log.Info().Int("pid", pid).Int("signal", int(signum)).Msg("sending signal")

	if err := clxc.setConfigItem("lxc.signal.stop", strconv.Itoa(int(signum))); err != nil {
		return err
	}
	if err := clxc.Container.Stop(); err != nil {
		return err
	}

	// send signal to the monitor process if it still exist
	if err := unix.Kill(pid, 0); err == nil {
		err := unix.Kill(pid, signum)
		// container process has already died
		if signum == unix.SIGKILL || signum == unix.SIGTERM {
			return nil
		}
		return err
	}
	return nil
}
