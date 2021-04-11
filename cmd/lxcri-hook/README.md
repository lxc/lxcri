# Hooks

* see https://github.com/opencontainers/runtime-spec/blob/master/config.md

## Notes

The OCI hooks wrapper will work in plain lxc containers because the
OCI state (state.json, hooks.json, config.json) is not available.

It's perfectly reasonable to run hooks directly from lxcri cli

OCI state must be bind mounted into the container.

## CreateRuntime

NOTE underspecified
conditions: mount namespace have been created, mount operations performed (all ?)

* when: before pivot_root, after namespace creation
* path: runtime namespace
* exec: runtime namespace

* maps to: lxc.hook.pre-start ? (mounts are not created)
* lxc.hook.pre-mount ? (container's fs namespace == mount namespace ?)

## CreateContainer

* when: before pivot_root, after mount namespace setup
* path: runtime namespace
* exec: container namespace

* maps to: lxc.hook.mount

## StartContainer

* when: before lxcri-init execs, after mounts are complete
* path: container namespace
* exec: container namespace

* maps to: lxc.hook.start

Run from `lxcri-init` the same way the user process is executed?

Bind mount hook launcher into container.
Create folder with environ/cmdline files for each hook.

## PostStart

* when: after syncfifo is unblocked
* path: runtime namespace
* exec: runtime namespace

* maps to: no LXC hook

Usually this is done manually after calling `lxc-start`
Run directory after unblocking the syncfifo in Runtime.Start
Set LXC_ environment variables ?

## PostStop

* when: after container delete / before delete returns
* path: runtime namespace
* exec: runtime namespace

* maps to: lxc.hook.destroy

Run directly in Runtime.Delete


### Solution 1

Add a cli command `hooks` with the container name and the hook as argument.

* Bad: hooks should not be accessible through the CLI because they
  should only be executed within defined runtime states.
  (simply hide the command from the help output ?)

* Bad: lxcri with all libraries must be available in the container for
  CreateContainer and StartContainer hooks.

### Idea 2

* Update the container state in runtime commands and serialize it to the runtime directory.

Extend / Update the state from the LXC hook environment variables.
Create a  single C binary that executes the hooks from the lxc hook.

Serialize hooks into a format that can be consumed by hooks
and started from 'liblxc' using a simple static C binary,
similar to `lxcri-init`.

Use the same mechanism `lxcri-init` uses to exec the hook
processes.

* Bind mount the hook directories, for hooks running in the
container namespace into the container.
e.g /.lxcri/hooks

lxc.hook.mount = lxcri-hook create-runtime


e.g create

{runtime_dir}/state.json

{runtime_dir}/hooks/create_runtime/1/cmdline
{runtime_dir}/hooks/create_runtime/1/environ

{runtime_dir}/hooks/create_runtime/2/cmdline
{runtime_dir}/hooks/create_runtime/2/environ

...

{runtime_dir}/hooks/create-container/1/cmdline
{runtime_dir}/hooks/create-container/2/environ



Pass state.json to executed process.


c tool can iterate over contents in the hook directory
and load and execute process and cmline
for each subfolder.

* can be implemented as go binary and as C binary ....

* timeout: set as additional environment variable e.g OCI_HOOK_TIMEOUT
