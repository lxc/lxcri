package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	// "github.com/apex/log"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"github.com/urfave/cli"

	lxc "gopkg.in/lxc/go-lxc.v2"
)

var stateCmd = cli.Command{
	Name:   "state",
	Usage:  "returns state of a container",
	Action: doState,
	ArgsUsage: `[containerID]

<containerID> is the ID of the container you want to know about.
`,
	Flags: []cli.Flag{},
}

func doState(ctx *cli.Context) error {
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

	status := "stopped"
	pid := 0
	if c.Running() {
		if checkHackyPreStart(c) == "started" {
			status = "running"
		}
		pid = c.InitPid()

		// need to detect 'created' per
		// https://github.com/opencontainers/runtime-spec/blob/v1.0.0-rc4/runtime.md#state
		// it means "the container process has neither exited nor executed the user-specified program"

		// if cmd name of the child of the init pid starts with "/bin/sh /fifo-wait" then we can say it's 'created'

		procChildrenFilename := fmt.Sprintf("/proc/%d/task/%d/children", pid, pid)
		childrenStr, err := ioutil.ReadFile(procChildrenFilename)
		if err != nil {
			return errors.Wrapf(err, "failed to read children from %s", procChildrenFilename)
		}
		children := strings.Split(strings.TrimSpace(string(childrenStr)), " ")

		if len(children) == 1 {
			childPid, err := strconv.Atoi(children[0])
			if err != nil {
				return errors.Wrapf(err, "failed to convert child pid")
			}
			procCmdlineFilename := fmt.Sprintf("/proc/%d/cmdline", childPid)
			cmdline, err := ioutil.ReadFile(procCmdlineFilename)
			if err != nil {
				return errors.Wrapf(err, "failed to read cmdline from %s", procCmdlineFilename)
			}

			cmdArgv := strings.Split(string(cmdline), "\x00")
			if len(cmdArgv) > 2 && cmdArgv[0] == "/bin/sh" && cmdArgv[1] == "/fifo-wait" {
				status = "created"
			}
		}
	}
	// bundlePath is the enclosing directory of the rootfs:
	// https://github.com/opencontainers/runtime-spec/blob/v1.0.0-rc4/bundle.md
	bundlePath := filepath.Dir(c.ConfigItem("lxc.rootfs.path")[0])
	annotations := map[string]string{}
	s := specs.State{
		Version:     CURRENT_OCI_VERSION,
		ID:          containerID,
		Status:      status,
		Pid:         pid,
		Bundle:      bundlePath,
		Annotations: annotations,
	}

	stateJson, err := json.Marshal(s)
	if err != nil {
		return errors.Wrap(err, "failed to marshal json")
	}
	fmt.Fprint(os.Stdout, string(stateJson))

	return nil
}
