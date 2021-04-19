package lxcri

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/drachenfels-de/lxcri/pkg/log"
	"github.com/drachenfels-de/lxcri/pkg/specki"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func mkdirTemp() (string, error) {
	// /tmp has permissions 1777
	// it should never be used as runtime or rootfs parent
	return os.MkdirTemp(os.Getenv("HOME"), "lxcri-test")
}

func newRuntime(t *testing.T) *Runtime {
	runtimeRoot, err := mkdirTemp()
	require.NoError(t, err)
	t.Logf("runtime root: %s", runtimeRoot)

	err = unix.Chmod(runtimeRoot, 0755)
	require.NoError(t, err)

	rt := &Runtime{
		Log:        log.ConsoleLogger(true, log.TraceLevel),
		Root:       runtimeRoot,
		LibexecDir: os.Getenv("LIBEXEC_DIR"),
		//MonitorCgroup: "lxcri-monitor.slice",
	}
	if rt.LibexecDir == "" {
		rt.LibexecDir = "/usr/local/libexec/lxcri"
	}
	//ExecInit = "lxcri-debug"
	require.NoError(t, rt.Init())
	return rt
}

// NOTE a container that was created successfully must always be
// deleted, otherwise the go test runner will hang because it waits
// for the container process to exit.
func newConfig(t *testing.T, cmd string, args ...string) *ContainerConfig {
	rootfs, err := mkdirTemp()
	require.NoError(t, err)
	t.Logf("container rootfs: %s", rootfs)

	// copy test binary to rootfs
	err = exec.Command("cp", cmd, rootfs).Run()
	require.NoError(t, err)

	spec := specki.NewSpec(rootfs, filepath.Join("/"+filepath.Base(cmd)))
	id := filepath.Base(rootfs)
	cfg := ContainerConfig{ContainerID: id, Spec: spec, Log: log.ConsoleLogger(true, log.TraceLevel)}
	cfg.Spec.Linux.CgroupsPath = id + ".slice" // use /proc/self/cgroup"

	// FIXME /dev/stderr has perms 600
	// If container process user is not equal to the
	// runtime process user then setting lxc log file will fail
	// because of missing permissions.
	if runAsRuntimeUser(cfg.Spec) {
		cfg.LogFile = "/dev/stderr"
	} else {
		cfg.LogFile = filepath.Join(rootfs, "log")
	}
	t.Logf("liblxc log output is written to %s", cfg.LogFile)
	cfg.LogLevel = "trace"

	return &cfg
}

func TestEmptyNamespaces(t *testing.T) {
	rt := newRuntime(t)
	defer os.RemoveAll(rt.Root)

	cfg := newConfig(t, "lxcri-test")
	defer os.RemoveAll(cfg.Spec.Root.Path)

	// Clearing all namespaces should not work,
	// since the mount namespace must never be shared with the host.
	cfg.Spec.Linux.Namespaces = cfg.Spec.Linux.Namespaces[0:0]

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	c, err := rt.Create(ctx, cfg)
	require.Error(t, err)
	t.Logf("create error: %s", err)
	require.Nil(t, c)
}

func TestSharedPIDNamespace(t *testing.T) {
	rt := newRuntime(t)
	defer os.RemoveAll(rt.Root)

	cfg := newConfig(t, "lxcri-test")
	defer os.RemoveAll(cfg.Spec.Root.Path)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	pidns := specs.LinuxNamespace{
		Type: specs.PIDNamespace,
		Path: fmt.Sprintf("/proc/%d/ns/pid", os.Getpid()),
	}

	for i, ns := range cfg.Spec.Linux.Namespaces {
		if ns.Type == specs.PIDNamespace {
			cfg.Spec.Linux.Namespaces[i] = pidns
		}
	}

	c, err := rt.Create(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, c)

	err = rt.Delete(ctx, c.ContainerID, true)
	require.NoError(t, err)
}

// TODO test uts namespace (shared with host)

func TestNonEmptyCgroup(t *testing.T) {
	rt := newRuntime(t)
	defer os.RemoveAll(rt.Root)

	cfg := newConfig(t, "lxcri-test")
	defer os.RemoveAll(cfg.Spec.Root.Path)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	c, err := rt.Create(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, c)

	cfg2 := newConfig(t, "lxcri-test")
	defer os.RemoveAll(cfg.Spec.Root.Path)
	cfg2.Spec.Linux.CgroupsPath = cfg.Spec.Linux.CgroupsPath

	c2, err := rt.Create(ctx, cfg2)
	require.Error(t, err)
	t.Logf("create error: %s", err)
	require.NotNil(t, c2)

	err = rt.Delete(ctx, c.ContainerID, true)
	require.NoError(t, err)
}

func TestRuntimePrivileged(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skipf("This tests only runs as root")
	}

	rt := newRuntime(t)
	defer os.RemoveAll(rt.Root)

	cfg := newConfig(t, "lxcri-test")
	defer os.RemoveAll(cfg.Spec.Root.Path)

	testRuntime(t, rt, cfg)
}

// The following tests require the following setup:

// sudo /bin/sh -c "echo '$(whoami):1000:1' >> /etc/subuid"
// sudo /bin/sh -c "echo '$(whoami):20000:65536' >> /etc/subuid"
// sudo /bin/sh -c "echo '$(whoami):1000:1' >> /etc/subgid"
// sudo /bin/sh -c "echo '$(whoami):20000:65536' >> /etc/subgid"
// sudo chown -R $(whoami):$(whoami) /sys/fs/cgroup/unified$(cat /proc/self/cgroup  | grep '^0:' | cut -d: -f3)
// sudo chown -R $(whoami):$(whoami) /sys/fs/cgroup$(cat /proc/self/cgroup  | grep '^0:' | cut -d: -f3)
//
func TestRuntimeUnprivileged(t *testing.T) {
	rt := newRuntime(t)
	defer os.RemoveAll(rt.Root)

	cfg := newConfig(t, "lxcri-test")
	defer os.RemoveAll(cfg.Spec.Root.Path)

	// The container UID must have full access to the rootfs.
	// MkdirTemp sets directory permissions to 0700.
	// If we the container UID (0) / or GID are not mapped to the owner (creator) of the rootfs,
	// then the rootfs and runtime directory permissions must be expanded.

	err := unix.Chmod(cfg.Spec.Root.Path, 0777)
	require.NoError(t, err)

	err = unix.Chmod(rt.Root, 0755)
	require.NoError(t, err)

	cfg.Spec.Linux.UIDMappings = []specs.LinuxIDMapping{
		specs.LinuxIDMapping{ContainerID: 0, HostID: 20000, Size: 65536},
	}
	cfg.Spec.Linux.GIDMappings = []specs.LinuxIDMapping{
		specs.LinuxIDMapping{ContainerID: 0, HostID: 20000, Size: 65536},
	}

	testRuntime(t, rt, cfg)
}

func TestRuntimeUnprivileged2(t *testing.T) {
	rt := newRuntime(t)
	defer os.RemoveAll(rt.Root)

	cfg := newConfig(t, "lxcri-test")
	defer os.RemoveAll(cfg.Spec.Root.Path)

	if os.Getuid() != 0 {
		cfg.Spec.Linux.UIDMappings = []specs.LinuxIDMapping{
			specs.LinuxIDMapping{ContainerID: 0, HostID: uint32(os.Getuid()), Size: 1},
			specs.LinuxIDMapping{ContainerID: 1, HostID: 20000, Size: 65536},
		}
		cfg.Spec.Linux.GIDMappings = []specs.LinuxIDMapping{
			specs.LinuxIDMapping{ContainerID: 0, HostID: uint32(os.Getgid()), Size: 1},
			specs.LinuxIDMapping{ContainerID: 1, HostID: 20000, Size: 65536},
		}
	}

	testRuntime(t, rt, cfg)
}

func testRuntime(t *testing.T, rt *Runtime, cfg *ContainerConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	c, err := rt.Create(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, c)

	state, err := c.State()
	require.NoError(t, err)
	require.Equal(t, specs.StateCreated, state.SpecState.Status)

	err = rt.Start(ctx, c)
	require.NoError(t, err)

	state, err = c.State()
	require.NoError(t, err)
	require.Equal(t, specs.StateRunning, state.SpecState.Status)

	// Must wait otherwise init process signal handlers may not
	// yet be established and then sending SIGHUP will kill the container
	//
	err = rt.Kill(ctx, c, unix.SIGUSR1)
	require.NoError(t, err)

	state, err = c.State()
	require.NoError(t, err)
	require.Equal(t, specs.StateRunning, state.SpecState.Status)

	time.Sleep(time.Millisecond * 500)

	// SIGHUP by default terminates a process if it is not ignored or catched by
	// a signal handler
	err = rt.Kill(ctx, c, unix.SIGKILL)
	require.NoError(t, err)

	time.Sleep(time.Millisecond * 50)

	state, err = c.State()
	require.NoError(t, err)
	require.Equal(t, specs.StateStopped, state.SpecState.Status)

	err = rt.Delete(ctx, c.ContainerID, false)
	require.NoError(t, err)

	state, err = c.State()
	require.NoError(t, err)
	require.Equal(t, specs.StateStopped, state.SpecState.Status)

	t.Log("done")

	err = c.Release()
	require.NoError(t, err)
}
