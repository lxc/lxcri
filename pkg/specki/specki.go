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
func RunHooks(ctx context.Context, state *specs.State, hooks []specs.Hook) error {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to serialize spec state: %w", err)
	}
	for i, h := range hooks {
		fmt.Printf("running hook[%d] path:%s\n", i, h.Path)
		if err := RunHook(ctx, stateJSON, h); err != nil {
			return err
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
