package lxcri

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/drachenfels-de/lxcri/log"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// Create a package test ? test.NewRuntime()

var tmpDir string

func init() {
	// /tmp has permissions 1777, never  use it as runtime / rootfs parent
	if os.Getuid() != 0 {
		tmpDir = os.Getenv("HOME")
	}
}

func newRuntime(t *testing.T) *Runtime {
	//wd, err := os.Getwd()
	//require.NoError(t, err)
	runtimeRoot, err := os.MkdirTemp(tmpDir, "lxcri-test")
	require.NoError(t, err)
	t.Logf("runtime root: %s", runtimeRoot)

	err = unix.Chmod(runtimeRoot, 0755)
	require.NoError(t, err)

	rt := &Runtime{
		Log:        log.ConsoleLogger(true),
		Root:       runtimeRoot,
		LibexecDir: os.Getenv("LIBEXEC_DIR"),
	}
	//ExecInit = "lxcri-debug"
	require.NoError(t, rt.Init())
	return rt
}

func newConfig(t *testing.T, cmd string, args ...string) *ContainerConfig {
	//wd, err := os.Getwd()
	//require.NoError(t, err)
	rootfs, err := os.MkdirTemp(tmpDir, "lxcri-test")
	require.NoError(t, err)
	t.Logf("container rootfs: %s", rootfs)

	// copy binary to rootfs
	err = exec.Command("cp", cmd, rootfs).Run()
	require.NoError(t, err)

	spec := NewSpec(rootfs, filepath.Join("/"+filepath.Base(cmd)))
	id := filepath.Base(rootfs)
	cfg := ContainerConfig{ContainerID: id, Spec: spec, Log: log.ConsoleLogger(true)}
	cfg.Linux.CgroupsPath = "" // use /proc/self/cgroup"
	cfg.LogFile = "/dev/stderr"
	cfg.LogLevel = "trace"

	if os.Getuid() != 0 {

		// container user ID 0 is mapped to user creating the container
		// --> file permissions in /.lxcri could be 0600 / 0400

		// get UID/GID mapping from /etc/subgid /etc/subuid

		/*
			cfg.Linux.UIDMappings = []specs.LinuxIDMapping{
				specs.LinuxIDMapping{ContainerID: 0, HostID: uint32(os.Getuid()), Size: 1},
				specs.LinuxIDMapping{ContainerID: 1, HostID: 20000, Size: 65536},
			}
			cfg.Linux.GIDMappings = []specs.LinuxIDMapping{
				specs.LinuxIDMapping{ContainerID: 0, HostID: uint32(os.Getgid()), Size: 1},
				specs.LinuxIDMapping{ContainerID: 1, HostID: 20000, Size: 65536},
			}
		*/

		// The container UID must have full access to the rootfs.
		// If we the container UID (0) / or GID are not mapped to the owner (creator) of the rootfs
		// it must be granted o+rwx.
		// MkdirTemp creates with 0700

		err = unix.Chmod(rootfs, 0777)
		require.NoError(t, err)

		// Using 0755 is required if container UID is not
		// the owner of runtimeRoot

		//err = unix.Chmod(runtimeRoot, 0755)
		//require.NoError(t, err)

		cfg.Linux.UIDMappings = []specs.LinuxIDMapping{
			specs.LinuxIDMapping{ContainerID: 0, HostID: 20000, Size: 65536},
		}
		cfg.Linux.GIDMappings = []specs.LinuxIDMapping{
			specs.LinuxIDMapping{ContainerID: 0, HostID: 20000, Size: 65536},
		}

	}

	/*
		cgroupns := specs.LinuxNamespace{
			Type: specs.CgroupNamespace,
		}
		cfg.Linux.Namespaces = append(cfg.Linux.Namespaces, cgroupns)
	*/

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

	time.Sleep(time.Second * 3)

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
