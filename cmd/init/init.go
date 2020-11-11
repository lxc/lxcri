package main

import (
	"github.com/lxc/crio-lxc/cmd/internal"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

func fail(err error, step string) {
	panic("init step [" + step + "] failed: " + err.Error())
}

func main() {
	spec, err := internal.ReadSpec(internal.INIT_SPEC)
	if err != nil {
		panic(err)
	}

	fifo, err := os.OpenFile(internal.SYNC_FIFO_PATH, os.O_WRONLY, 0)
	if err != nil {
		fail(err, "open sync fifo")
	}

	_, err = fifo.Write([]byte(internal.SYNC_FIFO_CONTENT))
	if err != nil {
		fail(err, "write to sync fifo")
	}

	env := setHome(spec.Process.Env, spec.Process.User.Username, spec.Process.Cwd)

	if err := unix.Chdir(spec.Process.Cwd); err != nil {
		fail(err, "change to cwd")
	}

	cmdPath, err := exec.LookPath(spec.Process.Args[0])
	if err != nil {
		fail(err, "lookup cmd path")
	}

	err = unix.Exec(cmdPath, spec.Process.Args, env)
	if err != nil {
		fail(err, "exec")
	}
}

func setHome(env []string, userName string, fallback string) []string {
	// Use existing HOME environment variable.
	for _, kv := range env {
		if strings.HasPrefix(kv, "HOME=") {
			return env
		}
		return env
	}
	// Or lookup users home directory in passwd.
	if userName != "" {
		u, err := user.Lookup(userName)
		if err == nil && u.HomeDir != "" {
			return append(env, "HOME="+u.HomeDir)
		}
	}
	// Use the provided fallback path as last resort.
	return append(env, "HOME="+fallback)
}
