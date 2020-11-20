# About

This is a wrapper around [LXC](https://github.com/lxc/lxc) which can be used as
a drop-in container runtime replacement for use by
[CRI-O](https://github.com/kubernetes-sigs/cri-o).

## Installation 

For the installation of the runtime see [INSTALL.md](INSTALL.md)</br>
For the installation and initialization of a kubernetes cluster see [K8S.md](K8S.md)

## Glossary

* `runtime` the crio-lxc binary and the command set that implement the [OCI runtime spec](https://github.com/opencontainers/runtime-spec/releases/download/v1.0.2/oci-runtime-spec-v1.0.2.html)
* `container process`  the process that starts and runs the container using liblxc (crio-lxc-start)

## Bugs

* cli: --help shows environment values not defaults https://github.com/urfave/cli/issues/1206

## Requirements and restrictions

* Only cgroupv2 unified cgroup hierarchy is supported.
* A recent kernel > 5.8 is required for full cgroup support.
* Cgroup resource limits are not implemented yet. This will change soon.
* runtime spec `additionalGroups` requires liblxc and go-lxc development version

## Configuration

The runtime binary implements flags that are required by the `OCI runtime spec`,</br>
and flags that are runtime specific (timeouts, hooks, logging ...).

Most of the runtime specific flags have corresponding environment variables. See `crio-lxc --help`.</br>
The runtime evaluates the flag value in the following order (lower order takes precedence).

1. cmdline flag from process arguments (overwrites process environment)
2. process environment variable (overwrites environment file)
3. environment file (overwrites cmdline flag default)
4. cmdline flag default

### Environment variables

Currently you have to compile to environment file yourself.</br>
To get all available variables 

```
grep EnvVars cmd/*.go | grep -o CRIO_LXC_[A-Za-z_]* | xargs -n1 -I'{}' echo "#{}="
```

###  Environment file

The default path to the environment file is `/etc/defaults/crio-lxc`.</br>
It is loaded on every start of the `crio-lxc` binary, so changes take immediate effect.</br>
Empty lines and those commented with a leading *#* are ignored.</br>

A malformed environment will let the next runtime call fail.</br>
In production it's recommended that you replace the environment file atomically.</br>

E.g the environment file `/etc/default/crio-lxc` could look like this:

```
#CRIO_LXC_CONTAINER_HOOK=
#CRIO_LXC_CREATE_TIMEOUT=30s
#CRIO_LXC_INIT_CMD=
#CRIO_LXC_START_CMD=
#CRIO_LXC_START_TIMEOUT=30s

CRIO_LXC_APPARMOR=true
CRIO_LXC_CAPABILITIES=true
CRIO_LXC_CGROUP_DEVICES=true
CRIO_LXC_SECCOMP=true

CRIO_LXC_LOG_FILE=/tmp/crio-lxc.log
CRIO_LXC_LOG_LEVEL=info
CRIO_LXC_CONTAINER_LOG_LEVEL=warn

CRIO_LXC_MONITOR_CGROUP=crio-lxc-monitor.slice
CRIO_LXC_RUNTIME_HOOK=/usr/local/bin/crio-lxc-backup.sh
#CRIO_LXC_RUNTIME_HOOK_RUN_ALWAYS=false
#CRIO_LXC_RUNTIME_HOOK_TIMEOUT=
```

### Runtime (security) features

All supported runtime security features are enabled by default.</br>
There following runtime (security) features can optionally be disabled.</br>
Details see `crio-lxc --help`

* apparmor
* capabilities
* cgroup-devices
* seccomp

### Logging

There is only a single log file for runtime and container process log output.</br>
The log-level for the runtime and the container process can be set independently.

* a single logfile is easy to rotate and monitor
* a single logfile is easy to tail (watch for errors / events ...)
* robust implementation is easy

#### Log Filtering

Runtime log lines are written in JSON using [zerolog](https://github.com/rs/zerolog).</br>
The log file can be easily filtered with [jq](https://stedolan.github.io/jq/).</br>
For filtering with  `jq` you must strip the container process logs with `grep -v '^lxc'`</br>

E.g Filter show only errors and warnings for runtime `create` command:

```sh
 grep -v '^lxc ' /var/log/crio-lxc.log |\
  jq -c 'select(.cmd == "create" and ( .l == "error or .l == "warn")'
```

#### Runtime log fields

Fields that are always present:

* `l` log level
* `m` log message
* `c` caller (source file and line number)
* `cid` container ID
* `cmd` runtime command
* `t` timestamp in UTC (format matches container process output)


Log message specific fields:

* `pid` a process ID
* `file` a path to a file
* `lxc.config` the key of a container process config item 
* `env` the key of an environment variable


### Debugging

Apart from the logfile following resources are useful:

* Systemd journal for cri-o and kubelet services 
* `coredumpctl` if runtime or container process segfaults. 

#### Runtime Hook

If a runtime hook is defined, it is executed when the `create` command returns with an error.</br>
You can use the runtime hook to backup the runtime spec and container process config for further analysis.</br>

The runtime hook executable must 

* not use the standard file descriptors (stdin/stdout/stderr) although they are nulled.
* not exceeds `CRIO_LXC_RUNTIME_HOOK_TIMEOUT` or it gets killed.
* not modify/delete any resources created by the runtime or container process

The runtime hook process environment contains the following variables:

* `CONTAINER_ID` the container ID
* `LXC_CONFIG` the path to runtime process config
* `RUNTIME_CMD` the runtime command which executed the runtime hook
* `RUNTIME_PATH` the path to the container runtime directory
* `BUNDLE_PATH` the absolute path to the container bundle
* `SPEC_PATH` the absolute path to the the JSON runtime spec
* `RUNTIME_ERROR` (optional) the error message if the runtime cmd return with error

Example environment of a shell script:

```
SPEC_PATH=/var/run/containers/storage/overlay-containers/XXX/userdata/config.json
PWD=/
RUNTIME_PATH=/run/crio-lxc/XXX
CONTAINER_ID=XXX
SHLVL=1
RUNTIME_CMD=create
BUNDLE_PATH=/var/run/containers/storage/overlay-containers/XXX/userdata
LXC_CONFIG=/run/crio-lxc/XXX/config
_=/usr/bin/env
```
