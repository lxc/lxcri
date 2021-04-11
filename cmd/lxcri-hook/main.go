package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func init() {
	// from `man lxc.container.conf`
	// Standard  output from the hooks is logged at debug level
	// Standard error is not logged, but can be captured by the hook
	// redirecting its standard error to standard output.
	os.Stderr = os.Stdout
}

func main() {

	var timeout int
	// Individual hooks should set a timeout lower than the overall timeout.
	flag.IntVar(&timeout, "timeout", 30, "maximum run time in seconds allowed for all hooks")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	env, err := LoadEnv()
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(2)
	}

	err = run(ctx, env)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(3)
	}
}

func run(ctx context.Context, env *Env) error {

	runtimeDir := filepath.Dir(env.ConfigFile)

	// TODO save hooks to hooks.json
	var hooks specs.Hooks
	hooksPath := filepath.Join(runtimeDir, "hooks.json")
	err := decodeFileJSON(&hooks, hooksPath)
	if err != nil {
		return err
	}

	//  TODO save state to state.json
	hooksToRun, status, err := ociHooksAndState(env.Type, &hooks)
	if err != nil {
		return err
	}

	if len(hooksToRun) == 0 {
		return fmt.Errorf("no hooks defined for %q", env.Type)
	}

	// need to deserialize it to set the current specs.ContainerState
	var state specs.State
	statePath := filepath.Join(runtimeDir, "state.json")
	err = decodeFileJSON(&hooks, statePath)
	if err != nil {
		return err
	}
	state.Status = status

	return runHooks(ctx, &state, hooksToRun)
}

// https://github.com/opencontainers/runtime-spec/blob/master/specs-go/state.go
// The only value that does change is the specs.ContainerState in specs.State.Status.
// The specs.ContainerState is implied by the runtime hook.
// status, and the status is already defined by the hook itself ...
func ociHooksAndState(t HookType, hooks *specs.Hooks) ([]specs.Hook, specs.ContainerState, error) {
	switch t {
	case HookPreMount:
		// quote from https://github.com/opencontainers/runtime-spec/blob/master/config.md#posix-platform-hooks
		// > For runtimes that implement the deprecated prestart hooks as createRuntime hooks,
		// > createRuntime hooks MUST be called after the prestart hooks.
		return append(hooks.Prestart, hooks.CreateRuntime...), specs.StateCreating, nil
	case HookMount:
		return hooks.CreateContainer, specs.StateCreating, nil
	case HookStart:
		return hooks.StartContainer, specs.StateCreated, nil
	// NOTE the following hooks are executed directly from lxcri
	//case HookPostStart:
	//	return hooks.Poststart, specs.StateRunning, nil
	//case HookDestroy:
	//	return hooks.Poststop, specs.StateStopped, nil
	default:
		return nil, specs.StateStopped, fmt.Errorf("liblxc hook %q is not mapped to OCI hooks", t)
	}
}

func runHooks(ctx context.Context, state *specs.State, hooks []specs.Hook) error {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to serialize spec state: %w", err)
	}
	for i, h := range hooks {
		fmt.Printf("running hook[%d] path:%s\n", i, h.Path)
		if err := runHook(ctx, stateJSON, h); err != nil {
			return err
		}
	}
	return nil
}

func runHook(ctx context.Context, stateJSON []byte, hook specs.Hook) error {
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

// FIXME copied from `lxcri`
func decodeFileJSON(obj interface{}, src string) error {
	// #nosec
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	// #nosec
	err = json.NewDecoder(f).Decode(obj)
	if err != nil {
		f.Close()
		return fmt.Errorf("failed to decode JSON from %s: %w", src, err)
	}
	err = f.Close()
	if err != nil {
		return fmt.Errorf("failed to close %s: %w", src, err)
	}
	return nil
}