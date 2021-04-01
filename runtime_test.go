package lxcri

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// Create a package test ? test.NewRuntime()

func newRuntime(t *testing.T) *Runtime {
	runtimeRoot, err := os.MkdirTemp("", "lxcri-test")
	require.NoError(t, err)
	t.Logf("runtime root: %s", runtimeRoot)

	return &Runtime{
		Log:        DefaultRuntime.Log,
		Root:       runtimeRoot,
		LibexecDir: "/usr/local/libexec/lxcri",
		Features:   DefaultRuntime.Features,
	}
}

func newConfig(t *testing.T, cmd string, args ...string) *ContainerConfig {
	rootfs, err := os.MkdirTemp("", "lxcri-test")
	require.NoError(t, err)
	t.Logf("container rootfs: %s", rootfs)

	// copy binary to rootfs
	err = exec.Command("cp", cmd, rootfs).Run()
	require.NoError(t, err)
	// create /proc and /dev in rootfs
	err = os.MkdirAll(filepath.Join(rootfs, "dev"), 0755)
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(rootfs, "proc"), 0755)
	require.NoError(t, err)

	spec := NewSpec(rootfs, filepath.Join("/"+filepath.Base(cmd)))
	id := filepath.Base(rootfs)
	cfg := ContainerConfig{ContainerID: id, Spec: spec}
	cfg.LogFile = "/dev/stderr"
	cfg.LogLevel = "info"
	return &cfg
}

func TestRuntimeNamespaceCheck(t *testing.T) {
	rt := newRuntime(t)
	defer os.RemoveAll(rt.Root)

	cfg := newConfig(t, "lxcri-test")
	defer os.RemoveAll(cfg.Root.Path)

	// Clearing all namespaces should not work.
	// At least PID and MOUNT must not be shared with the host.
	cfg.Linux.Namespaces = cfg.Linux.Namespaces[0:0]

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	c, err := rt.Create(ctx, cfg)
	require.Error(t, err)
	require.Nil(t, c)

	pidns := specs.LinuxNamespace{
		Type: specs.PIDNamespace,
		Path: fmt.Sprintf("/proc/%d/ns/pid", os.Getpid()),
	}
	cfg.Linux.Namespaces = append(cfg.Linux.Namespaces, pidns)

	c, err = rt.Create(ctx, cfg)
	require.Error(t, err)
	require.Nil(t, c)
}

func TestRuntimeKill(t *testing.T) {
	rt := newRuntime(t)
	defer os.RemoveAll(rt.Root)

	cfg := newConfig(t, "lxcri-test")
	defer os.RemoveAll(cfg.Root.Path)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	c, err := rt.Create(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, c)

	state, err := c.State()
	require.NoError(t, err)
	require.Equal(t, specs.StateCreated, state.Status)

	err = rt.Start(ctx, c)
	require.NoError(t, err)

	state, err = c.State()
	require.NoError(t, err)
	require.Equal(t, specs.StateRunning, state.Status)

	// Must wait otherwise init process signal handlers may not
	// yet be established and then sending SIGHUP will kill the container
	//
	err = rt.Kill(ctx, c, unix.SIGUSR1)
	require.NoError(t, err)

	state, err = c.State()
	require.NoError(t, err)
	require.Equal(t, specs.StateRunning, state.Status)

	// SIGHUP by default terminates a process if it is not ignored or catched by
	// a signal handler
	err = rt.Kill(ctx, c, unix.SIGKILL)
	require.NoError(t, err)

	time.Sleep(time.Millisecond * 50)

	state, err = c.State()
	require.NoError(t, err)
	require.Equal(t, specs.StateStopped, state.Status)

	err = rt.Delete(ctx, c, false)
	require.NoError(t, err)

	state, err = c.State()
	require.NoError(t, err)
	require.Equal(t, specs.StateStopped, state.Status)

	t.Log("done")

	err = c.Release()
	require.NoError(t, err)

	/*
		p, err := os.FindProcess(c.Pid)
		require.NoError(t, err)
		pstate, err := p.Wait()
		require.NoError(t, err)
		// The exit code should be non-zero because the process was terminated
		// by a signal
		fmt.Printf("monitor process exited %T: %s code:%d\n", pstate, pstate, pstate.ExitCode())
	*/

	// manpage for 'wait4' suggests to use waitpid or waitid instead, but
	// golang/x/sys/unix only implements 'Wait4'. See https://github.com/golang/go/issues/9176
	var ws unix.WaitStatus
	_, err = unix.Wait4(c.Pid, &ws, 0, nil)
	require.NoError(t, err)
	fmt.Printf("ws:0x%x exited:%t exit_status:%d signaled:%t signal:%d\n", ws, ws.Exited(), ws.ExitStatus(), ws.Signaled(), ws.Signal())

	// NOTE it seems that the go test framework reaps all remaining children.
	// It's reasonable that no process started from the tests will survive the test run.
}
