package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"time"

	"github.com/drachenfels-de/lxcri/pkg/specki"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

func main() {
	// TODO use environment variable for runtime dir
	runtimeDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get runtime dir: %s\n", err)
		os.Exit(2)
	}

	specPath := filepath.Join(runtimeDir, "config.json")
	spec, err := specki.ReadSpecJSON(specPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(3)
	}

	err = doInit(runtimeDir, spec)
	if err != nil {
		if err := writeTerminationLog(spec, "init failed: %s\n", err); err != nil {
			fmt.Fprintf(os.Stderr, "%s", err)
		}
		fmt.Fprintf(os.Stderr, "init failed: %s\n", err)
		os.Exit(4)
	}
}

func writeTerminationLog(spec *specs.Spec, format string, a ...interface{}) error {
	var terminationLog string
	if spec.Annotations != nil {
		terminationLog = spec.Annotations["io.kubernetes.container.terminationMessagePath"]
	}
	if terminationLog == "" {
		return nil
	}

	f, err := os.OpenFile(terminationLog, os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, format, a...)
	if err != nil {
		return fmt.Errorf("failed to write to termination log %q: %w", terminationLog, err)
	}
	return nil
}

func doInit(runtimeDir string, spec *specs.Spec) error {
	statePath := filepath.Join(runtimeDir, "state.json")
	state, err := specki.LoadSpecStateJSON(statePath)
	if err != nil {
		return fmt.Errorf("failed to read spec %q: %s", statePath, err)
	}

	cmdPath := spec.Process.Args[0]
	val, exist := specki.Getenv(spec.Process.Env, "PATH")
	if exist {
		err := os.Setenv("PATH", val)
		if err != nil {
			return fmt.Errorf("failed to set PATH environment variable: %s", err)
		}
		cmdPath, err = exec.LookPath(spec.Process.Args[0])
		if err != nil {
			return fmt.Errorf("lookup path for %s failed: %w", spec.Process.Args[0], err)
		}
	}

	_, exist = specki.Getenv(spec.Process.Env, "HOME")
	if !exist {
		addEnvHome(spec)
	}

	err = unix.Chdir(spec.Process.Cwd)
	if err != nil {
		return fmt.Errorf("failed to change cwd to %s: %w", spec.Process.Cwd, err)
	}

	err = readSyncfifo(filepath.Join(runtimeDir, "syncfifo"))
	if err != nil {
		return err
	}

	// TODO use environment variable to control timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	err = specki.RunHooks(ctx, state, spec.Hooks.StartContainer, false)
	if err != nil {
		return err
	}

	unix.Exec(cmdPath, spec.Process.Args, spec.Process.Env)
	if err != nil {
		return fmt.Errorf("exec failed: %w", err)
	}
	return nil
}

func readSyncfifo(filename string) error {
	f, err := os.OpenFile(filename, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", filename, err)
	}
	return f.Close()
}

/*
func closeExtraFds() {
	os.Open("/proc/self/fd")
}
*/

func addEnvHome(spec *specs.Spec) {
	// lookup users home directory in passwd.
	userName := spec.Process.User.Username
	if userName != "" {
		u, err := user.Lookup(userName)
		if err == nil && u.HomeDir != "" {
			spec.Process.Env = append(spec.Process.Env, "HOME="+u.HomeDir)
			return
		}
	}
	// If user is root without entry in /etc/passwd then try /root
	if spec.Process.User.UID == 0 {
		stat, err := os.Stat("/root")
		if err == nil && stat.IsDir() {
			spec.Process.Env = append(spec.Process.Env, "HOME=/root")
			return
		}
	}
	spec.Process.Env = append(spec.Process.Env, "HOME="+spec.Process.Cwd)
}
