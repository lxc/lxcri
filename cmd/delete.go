package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"

	"golang.org/x/sys/unix"
	lxc "gopkg.in/lxc/go-lxc.v2"
)

var deleteCmd = cli.Command{
	Name:   "delete",
	Usage:  "deletes a container",
	Action: doDelete,
	ArgsUsage: `[containerID]

<containerID> is the ID of the container to delete
`,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "force",
			Usage: "force deletion",
		},
	},
}

func doDelete(ctx *cli.Context) error {
	err := clxc.loadContainer()
	if err == errContainerNotExist && ctx.Bool("force") {
		return nil
	}
	if err != nil {
		return err
	}
	c := clxc.Container

	state := c.State()
	if state != lxc.STOPPED {
		if !ctx.Bool("force") {
			return fmt.Errorf("container must be stopped before delete (current state is %s)", state)
		}

		if err := c.Stop(); err != nil {
			return errors.Wrap(err, "failed to stop container")
		}
	}

	if err := c.Destroy(); err != nil {
		return errors.Wrap(err, "failed to delete container")
	}

	if dir := clxc.getConfigItem("lxc.cgroup.dir"); dir != "" {
		if err := tryRemoveAllCgroupDir(c, dir, true); err != nil {
			log.Warn().Err(err).Msg("remove lxc.cgroup.dir failed")
		} else {
			// try to remove outer directory, in case this is the POD that is deleted
			// FIXME crio should delete the kubepods slice
			tryRemoveAllCgroupDir(c, filepath.Dir(dir), false)
		}
	}

	if dir := clxc.getConfigItem("lxc.cgroup.dir.container"); dir != "" {
		if err := tryRemoveAllCgroupDir(c, dir, true); err != nil {
			log.Warn().Err(err).Msg("remove lxc.cgroup.dir.container failed")
		} else {
			// try to remove outer directory, in case this is the POD that is deleted
			// FIXME crio should delete the kubepods slice
			tryRemoveAllCgroupDir(c, filepath.Dir(dir), false)
		}
	}

	// "Note that resources associated with the container,
	// but not created by this container, MUST NOT be deleted."
	// TODO - because we set rootfs.managed=0, Destroy() doesn't
	// delete the /var/lib/lxc/$containerID/config file:
	return os.RemoveAll(clxc.runtimePath())
}

func tryRemoveAllCgroupDir(c *lxc.Container, cgroupPath string, killProcs bool) error {
	dirName := filepath.Join("/sys/fs/cgroup", cgroupPath)
	dir, err := os.Open(dirName)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if killProcs {
		err := loopKillCgroupProcs(dirName, time.Second*2)
		if err != nil {
			log.Trace().Err(err).Str("dir:", dirName).Msg("failed to kill cgroup procs")
		}
	}
	entries, err := dir.Readdir(-1)
	if err != nil {
		return err
	}
	// leftover lxc.pivot path
	for _, i := range entries {
		if i.IsDir() && i.Name() != "." && i.Name() != ".." {
			fullPath := filepath.Join(dirName, i.Name())
			if err := unix.Rmdir(fullPath); err != nil {
				return errors.Wrapf(err, "failed rmdir %s", fullPath)
			}
		}
	}
	return unix.Rmdir(dirName)
}

// loopKillCgroupProcs loops over PIDs in cgroup.procs and sends
// each PID the kill signal until there are no more PIDs left.
// Looping is required because processes that have been created (forked / exec)
// may not 'yet' be visible in cgroup.procs.
func loopKillCgroupProcs(scope string, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return fmt.Errorf("timeout killing processes")
		default:
			nprocs, err := killCgroupProcs(scope)
			if err != nil {
				return err
			}
			if nprocs == 0 {
				return nil
			}
			time.Sleep(time.Millisecond * 50)
		}
	}
}

// getCgroupProcs returns the PIDs for all processes which are in the
// same control group as the process for which the PID is given.
func killCgroupProcs(scope string) (int, error) {
	cgroupProcsPath := filepath.Join(scope, "cgroup.procs")
	log.Trace().Str("path:", cgroupProcsPath).Msg("reading control group process list")
	procsData, err := ioutil.ReadFile(cgroupProcsPath)
	if err != nil {
		return -1, errors.Wrapf(err, "failed to read control group process list %s", cgroupProcsPath)
	}
	// cgroup.procs contains one PID per line and is newline separated.
	// A trailing newline is always present.
	s := strings.TrimSpace(string(procsData))
	if s == "" {
		return 0, nil
	}
	pidStrings := strings.Split(s, "\n")
	numPids := len(pidStrings)
	if numPids == 0 {
		return 0, nil
	}

	// This indicates improper signal handling / termination of the container.
	log.Warn().Strs("pids:", pidStrings).Str("cgroup:", scope).Msg("killing left-over container processes")

	for _, s := range pidStrings {
		pid, err := strconv.Atoi(s)
		if err != nil {
			// Reading garbage from cgroup.procs should not happen.
			return -1, errors.Wrapf(err, "failed to convert PID %q to number", s)
		}
		if err := unix.Kill(pid, 9); err != nil {
			return -1, errors.Wrapf(err, "failed to kill %d", pid)
		}
	}

	return numPids, nil
}
