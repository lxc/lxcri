# About

`lxcri` is a wrapper around [LXC](https://github.com/lxc/lxc) which can be used as
a drop-in container runtime replacement for use by [CRI-O](https://github.com/kubernetes-sigs/cri-o).

### History

The project started as a fork of lxc/crio-lxc but has undergone several refactorings
and yet shares very little code with lxc/crio-lxc and was therefore renamed to `lxcri`

### OCI compliance

With liblxc >= https://github.com/lxc/lxc/commit/b5daeddc5afce1cad4915aef3e71fdfe0f428709  it passes all sonobuoy conformance tests.

## Installation

For the installation of the runtime see [INSTALL.md](INSTALL.md)</br>
For the installation and initialization of a kubernetes cluster see [K8S.md](K8S.md)

## Glossary

* `runtime` the lxcri binary and the command set that implement the [OCI runtime spec](https://github.com/opencontainers/runtime-spec/releases/download/v1.0.2/oci-runtime-spec-v1.0.2.html)
* `container process`  the process that starts and runs the container using liblxc (lxcri-start)
* `container config` the LXC config file
* `bundle config` the lxcri container state (bundle path, pidfile ....)
* `runtime spec` the OCI runtime spec from the bundle

## Bugs

* cli: --help shows environment values not defaults https://github.com/urfave/cli/issues/1206

## Requirements and restrictions

* Only cgroupv2 (unified cgroup hierarchy) is supported.
* A recent kernel >= 5.8 is required for full cgroup support.

### Unimplemented features

* [runtime: Implement POSIX platform hooks](https://github.com/Drachenfels-GmbH/lxcri/issues/10)
* [runtime: Implement cgroup2 resource limits](https://github.com/Drachenfels-GmbH/lxcri/issues/11)

## Configuration

The runtime binary implements flags that are required by the `OCI runtime spec`,</br>
and flags that are runtime specific (timeouts, hooks, logging ...).

Most of the runtime specific flags have corresponding environment variables. See `lxcri --help`.</br>
The runtime evaluates the flag value in the following order (lower order takes precedence).

1. cmdline flag from process arguments (overwrites process environment)
2. process environment variable (overwrites environment file)
3. environment file (overwrites cmdline flag default)
4. cmdline flag default

### Environment variables

Currently you have to compile to environment file yourself.</br>
To list  all available variables:

```
grep EnvVars cmd/cli.go | grep -o LXCRI_[A-Za-z_]* | xargs -n1 -I'{}' echo "#{}="
```

###  Environment file

The default path to the environment file is `/etc/defaults/lxcri`.</br>
It is loaded on every start of the `lxcri` binary, so changes take immediate effect.</br>
Empty lines and those commented with a leading *#* are ignored.</br>

A malformed environment will let the next runtime call fail.</br>
In production it's recommended that you replace the environment file atomically.</br>

E.g the environment file `/etc/default/lxcri` could look like this:

```sh
LXCRI_LOG_LEVEL=debug
LXCRI_CONTAINER_LOG_LEVEL=debug
#LXCRI_LOG_FILE=
#LXCRI_LOG_TIMESTAMP=
#LXCRI_MONITOR_CGROUP=
#LXCRI_INIT_EXEC=
#LXCRI_START_EXEC=
#LXCRI_HOOK_EXEC=
#LXCRI_APPARMOR=
#LXCRI_CAPABILITIES=
#LXCRI_CGROUP_DEVICES=
#LXCRI_SECCOMP=
#LXCRI_CREATE_TIMEOUT=
#LXCRI_CREATE_HOOK=/usr/local/bin/lxcri-backup.sh
#LXCRI_CREATE_HOOK_TIMEOUT=
#LXCRI_START_TIMEOUT=
#LXCRI_KILL_TIMEOUT=
#LXCRI_DELETE_TIMEOUT=
```

### Runtime (security) features

All supported runtime security features are enabled by default.</br>
The following runtime (security) features can optionally be disabled.</br>
Details see `lxcri --help`

* apparmor
* capabilities
* cgroup-devices
* seccomp

### Logging

There is only a single log file for runtime and container process log output.</br>
The log-level for the runtime and the container process can be set independently.

* containers are ephemeral, but the log file should not be
* a single logfile is easy to rotate and monitor
* a single logfile is easy to tail (watch for errors / events ...)
* robust implementation is easy

#### Log Filtering

Runtime log lines are written in JSON using [zerolog](https://github.com/rs/zerolog).</br>
The log file can be easily filtered with [jq](https://stedolan.github.io/jq/).</br>
For filtering with  `jq` you must strip the container process logs with `grep -v '^lxc'`</br>

E.g Filter show only errors and warnings for runtime `create` command:

```sh
 grep -v '^lxc ' /var/log/lxcri.log |\
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

#### Create Hook

If a create hook is defined, it is executed before the `create` command returns.</br>
You can use it to backup the runtime spec and container process config for further analysis.</br>

The create hook executable must

* not use the standard file descriptors (stdin/stdout/stderr) although they are nulled.
* not exceed `LXCRI_CREATE_HOOK_TIMEOUT` or it is killed.
* not modify/delete any resources created by the runtime or container process

The process environment contains the following variables:

* `CONTAINER_ID` the container ID
* `LXC_CONFIG` the path to runtime process config
* `RUNTIME_CMD` the runtime command which executed the runtime hook
* `RUNTIME_PATH` the path to the container runtime directory
* `BUNDLE_PATH` the absolute path to the container bundle
* `SPEC_PATH` the absolute path to the the JSON runtime spec
* `LOG_FILE` the path to the log file
* `RUNTIME_ERROR` (optional) the error message if the runtime cmd return with error

Example script `lxcri-backup.sh` that backs up any container runtime directory:

```sh
#!/bin/sh

LOGDIR=$(dirname $LOG_FILE)
OUT=$LOGDIR/$CONTAINER_ID

# backup container runtime directory to log directory
cp -r $RUNTIME_PATH $OUT
# copy OCI runtime spec to container runtime directory
cp $SPEC_PATH $OUT/spec.json

# remove non `grep` friendly runtime files (symlinks, binaries, fifos)
rm $OUT/.lxcri/cwd
rm $OUT/.lxcri/init
rm $OUT/.lxcri/syncfifo
```
