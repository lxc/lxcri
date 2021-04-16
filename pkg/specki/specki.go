// Package specki containse general-purpose helper functions that operate
// on (parts of) the runtime spec (specs.Spec).
// These functions should not contain any code that is `lxcri` specific.
package specki

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// UnmapContainerID returns the (user/group) ID to which the given
// ID is mapped to by the given idmaps.
// The returned id will be equal to the given id
// if it is not mapped by the given idmaps.
func UnmapContainerID(id uint32, idmaps []specs.LinuxIDMapping) uint32 {
	for _, idmap := range idmaps {
		if idmap.Size < 1 {
			continue
		}
		maxID := idmap.ContainerID + idmap.Size - 1
		// check if c.Process.UID is contained in the mapping
		if (id >= idmap.ContainerID) && (id <= maxID) {
			offset := id - idmap.ContainerID
			hostid := idmap.HostID + offset
			return hostid
		}
	}
	// uid is not mapped
	return id
}

// RunHooks calls RunHook for each of the given runtime hooks.
// The given runtime state is serialized as JSON and passed to each RunHook call.
func RunHooks(ctx context.Context, state *specs.State, hooks []specs.Hook, continueOnError bool) error {
	if len(hooks) == 0 {
		return nil
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to serialize spec state: %w", err)
	}
	for i, h := range hooks {
		fmt.Printf("running hook[%d] path:%s\n", i, h.Path)
		err := RunHook(ctx, stateJSON, h)
		if err != nil {
			fmt.Printf("hook[%d] failed: %s\n", i, err)
			if !continueOnError {
				return err
			}
		}
	}
	return nil
}

// RunHook executes the command defined by the given hook.
// The given runtime state is passed over stdin to the executed command.
// The command is executed with the given context ctx, or a sub-context
// of it if Hook.Timeout is not nil.
func RunHook(ctx context.Context, stateJSON []byte, hook specs.Hook) error {
	if hook.Timeout != nil {
		hookCtx, cancel := context.WithTimeout(ctx, time.Second*time.Duration(*hook.Timeout))
		defer cancel()
		ctx = hookCtx
	}
	cmd := exec.CommandContext(ctx, hook.Path, hook.Args...)
	cmd.Env = hook.Env
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	in, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if _, err := io.Copy(in, bytes.NewReader(stateJSON)); err != nil {
		return err
	}
	in.Close()
	return cmd.Wait()
}

// DecodeJSONFile reads the next JSON-encoded value from
// the file with the given filename and stores it in the value pointed to by v.
func DecodeJSONFile(filename string, v interface{}) error {
	// #nosec
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	// #nosec
	err = json.NewDecoder(f).Decode(v)
	if err != nil {
		f.Close()
		return fmt.Errorf("failed to decode JSON from %s: %w", filename, err)
	}
	err = f.Close()
	if err != nil {
		return fmt.Errorf("failed to close %s: %w", filename, err)
	}
	return nil
}

// EncodeJSONFile writes the JSON encoding of v followed by a newline character
// to the file with the given filename.
// The file is opened read-write with the (optional) provided flags.
// The permission bits perm (not affected by umask) are set after the file was closed.
func EncodeJSONFile(filename string, v interface{}, flags int, perm os.FileMode) error {
	f, err := os.OpenFile(filename, os.O_RDWR|flags, perm)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	err = enc.Encode(v)
	if err != nil {
		f.Close()
		return fmt.Errorf("failed to encode JSON to %s: %w", filename, err)
	}
	err = f.Close()
	if err != nil {
		return fmt.Errorf("failed to close %s: %w", filename, err)
	}
	// Use chmod because initial perm is affected by umask and flags.
	err = os.Chmod(filename, perm)
	if err != nil {
		return fmt.Errorf("failed to 'chmod %o %s': %w", perm, filename, err)
	}
	return nil
}

func int64p(v int64) *int64 {
	return &v
}

func modep(m os.FileMode) *os.FileMode {
	return &m
}

// FIXME runtime mandates that /dev/ptmx should be bind mount from host - why ?
// `man 2 mount` | devpts
// ` To use this option effectively, /dev/ptmx must be a symbolic link to pts/ptmx.
// See Documentation/filesystems/devpts.txt in the Linux kernel source tree for details.`
var (
	EssentialDevices = []specs.LinuxDevice{
		specs.LinuxDevice{Type: "c", Major: 1, Minor: 3, FileMode: modep(0666), Path: "/dev/null"},
		specs.LinuxDevice{Type: "c", Major: 1, Minor: 5, FileMode: modep(0666), Path: "/dev/zero"},
		specs.LinuxDevice{Type: "c", Major: 1, Minor: 7, FileMode: modep(0666), Path: "/dev/full"},
		specs.LinuxDevice{Type: "c", Major: 1, Minor: 8, FileMode: modep(0666), Path: "/dev/random"},
		specs.LinuxDevice{Type: "c", Major: 1, Minor: 9, FileMode: modep(0666), Path: "/dev/urandom"},
		specs.LinuxDevice{Type: "c", Major: 5, Minor: 0, FileMode: modep(0666), Path: "/dev/tty"},
	}

	EssentialDevicesAllow = []specs.LinuxDeviceCgroup{
		specs.LinuxDeviceCgroup{Allow: true, Type: "c", Major: int64p(1), Minor: int64p(3), Access: "rwm"}, // null
		specs.LinuxDeviceCgroup{Allow: true, Type: "c", Major: int64p(1), Minor: int64p(5), Access: "rwm"}, // zero
		specs.LinuxDeviceCgroup{Allow: true, Type: "c", Major: int64p(1), Minor: int64p(7), Access: "rwm"}, // full
		specs.LinuxDeviceCgroup{Allow: true, Type: "c", Major: int64p(1), Minor: int64p(8), Access: "rwm"}, // random
		specs.LinuxDeviceCgroup{Allow: true, Type: "c", Major: int64p(1), Minor: int64p(9), Access: "rwm"}, // urandom
		specs.LinuxDeviceCgroup{Allow: true, Type: "c", Major: int64p(5), Minor: int64p(0), Access: "rwm"}, // tty
		specs.LinuxDeviceCgroup{Allow: true, Type: "c", Major: int64p(5), Minor: int64p(2), Access: "rwm"}, // ptmx
		specs.LinuxDeviceCgroup{Allow: true, Type: "c", Major: int64p(88), Access: "rwm"},                  // /dev/pts/{n}
	}
)

// AllowEssentialDevices adds and allows access to EssentialDevices which are required by the
// [runtime spec](https://github.com/opencontainers/runtime-spec/blob/master/config-linux.md#default-devices)
func AllowEssentialDevices(spec *specs.Spec) error {
	for _, dev := range EssentialDevices {
		exist, err := IsDeviceEnabled(spec, dev)
		if err != nil {
			return err
		}
		if !exist {
			spec.Linux.Devices = append(spec.Linux.Devices, dev)
		}
	}

	for _, perm := range EssentialDevicesAllow {
		spec.Linux.Resources.Devices = append(spec.Linux.Resources.Devices, perm)
	}
	return nil
}

// IsDeviceEnabled checks if the LinuxDevice dev is enabled in the Spec spec.
// An error is returned if the device Path matches and Type, Major or Minor don't match.
func IsDeviceEnabled(spec *specs.Spec, dev specs.LinuxDevice) (bool, error) {
	for _, d := range spec.Linux.Devices {
		if d.Path == dev.Path {
			if d.Type != dev.Type {
				return false, fmt.Errorf("%s type mismatch (expected %s but was %s)", dev.Path, dev.Type, d.Type)
			}
			if d.Major != dev.Major {
				return false, fmt.Errorf("%s major number mismatch (expected %d but was %d)", dev.Path, dev.Major, d.Major)
			}
			if d.Minor != dev.Minor {
				return false, fmt.Errorf("%s major number mismatch (expected %d but was %d)", dev.Path, dev.Major, d.Major)
			}
			return true, nil
		}
	}
	return false, nil
}

// ReadSpecJSON reads the JSON encoded OCI
// spec from the given path.
// This is a convenience function for the cli.
func ReadSpecJSON(p string) (*specs.Spec, error) {
	spec := new(specs.Spec)
	err := DecodeJSONFile(p, spec)
	return spec, err
}

// ReadSpecProcessJSON reads the JSON encoded OCI
// spec process definition from the given path.
// This is a convenience function for the cli.
func ReadSpecProcessJSON(src string) (*specs.Process, error) {
	proc := new(specs.Process)
	err := DecodeJSONFile(src, proc)
	return proc, err
}

// LoadSpecProcess calls ReadSpecProcessJSON if the given specProcessPath is not empty,
// otherwise it creates a new specs.Process from the given args.
// It's an error if both values are empty.
func LoadSpecProcess(specProcessPath string, args []string) (*specs.Process, error) {
	if specProcessPath != "" {
		return ReadSpecProcessJSON(specProcessPath)
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("spec process path and args are empty")
	}
	return &specs.Process{Cwd: "/", Args: args}, nil
}

// NewSpec returns a minimal spec.Spec instance, which is
// required to run the given process within a container
// using the given rootfs.
// NOTE /proc and /dev folders must be present within the given rootfs.
func NewSpec(rootfs string, cmd string, args ...string) *specs.Spec {
	proc := NewSpecProcess(cmd, args...)

	return &specs.Spec{
		Version: specs.Version,
		Linux: &specs.Linux{
			Namespaces: []specs.LinuxNamespace{
				// isolate all namespaces by default
				specs.LinuxNamespace{Type: specs.PIDNamespace},
				specs.LinuxNamespace{Type: specs.MountNamespace},
				specs.LinuxNamespace{Type: specs.IPCNamespace},
				specs.LinuxNamespace{Type: specs.UTSNamespace},
				specs.LinuxNamespace{Type: specs.CgroupNamespace},
				specs.LinuxNamespace{Type: specs.NetworkNamespace},
			},
			Devices: EssentialDevices,
			Resources: &specs.LinuxResources{
				Devices: EssentialDevicesAllow,
			},
		},
		Mounts: []specs.Mount{
			specs.Mount{Destination: "/proc", Source: "proc", Type: "proc",
				Options: []string{"rw", "nosuid", "nodev", "noexec", "relatime"},
			},
			specs.Mount{Destination: "/dev", Source: "tmpfs", Type: "tmpfs",
				Options: []string{"rw", "nosuid", "noexec", "relatime", "dev"},
				// devtmpfs (rw,nosuid,relatime,size=6122620k,nr_inodes=1530655,mode=755,inode64)
			},
		},
		Process: proc,
		Root:    &specs.Root{Path: rootfs},
	}
}

// NewSpecProcess creates a specs.Process instance
// from the given command cmd and the command arguments args.
func NewSpecProcess(cmd string, args ...string) *specs.Process {
	proc := new(specs.Process)
	proc.Args = append(proc.Args, cmd)
	proc.Args = append(proc.Args, args...)
	proc.Cwd = "/"
	return proc
}

// LoadSpecStateJSON parses specs.State from the JSON encoded file filename.
func LoadSpecStateJSON(filename string) (*specs.State, error) {
	state := new(specs.State)
	err := DecodeJSONFile(filename, state)
	return state, err
}

// ReadSpecStateJSON parses the JSON encoded specs.State from the given reader.
func ReadSpecStateJSON(r io.Reader) (*specs.State, error) {
	state := new(specs.State)
	dec := json.NewDecoder(r)
	err := dec.Decode(state)
	return state, err
}

// InitHook is a convenience function for OCI hooks.
// It parses specs.State from the given reader and
// loads specs.Spec from the specs.State.Bundle path.
func InitHook(r io.Reader) (rootfs string, state *specs.State, spec *specs.Spec, err error) {
	state, err = ReadSpecStateJSON(r)
	if err != nil {
		return
	}
	spec, err = ReadSpecJSON(filepath.Join(state.Bundle, "config.json"))

	// quote from https://github.com/opencontainers/runtime-spec/blob/master/config.md#root
	// > On POSIX platforms, path is either an absolute path or a relative path to the bundle.
	// > For example, with a bundle at /to/bundle and a root filesystem at /to/bundle/rootfs,
	// > the path value can be either /to/bundle/rootfs or rootfs.
	// > The value SHOULD be the conventional rootfs.
	rootfs = spec.Root.Path
	if !filepath.IsAbs(rootfs) {
		rootfs = filepath.Join(state.Bundle, rootfs)
	}
	return
}

// BindMount returns a specs.Mount to bind mount src to dest.
// The given mount options opts are merged with the predefined options
// ("bind", "nosuid", "nodev", "relatime")
func BindMount(src string, dest string, opts ...string) specs.Mount {
	return specs.Mount{
		Source: src, Destination: dest, Type: "bind",
		Options: append([]string{"bind", "nosuid", "nodev", "relatime"}, opts...),
	}
}

func hasOption(m specs.Mount, opt string) bool {
	for _, o := range m.Options {
		if o == opt {
			return true
		}
	}
	return false
}

// HasOptions returns true if the given Mount has all provided options opts.
func HasOptions(m specs.Mount, opts ...string) bool {
	for _, o := range opts {
		if !hasOption(m, o) {
			return false
		}
	}
	return true
}

// Getenv returns the first matching value from env,
// which has a prefix of key + "=".
func Getenv(env []string, key string) (string, bool) {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			val := strings.TrimPrefix(kv, key+"=")
			return val, true
		}
	}
	return "", false
}

// Setenv adds the given variable to the environment env.
// The variable is only added if it is not yet defined
// or if overwrite is set to true.
// Setenv returns the modified environment and
// true the variable is already defined or false otherwise.
func Setenv(env []string, val string, overwrite bool) ([]string, bool) {
	a := strings.Split(val, "=")
	key := a[0]
	for i, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			if overwrite {
				env[i] = val
			}
			return env, true
		}
	}
	return append(env, val), false
}
