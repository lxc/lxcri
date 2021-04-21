## Glossary

* `runtime` the lxcri binary and the command set that implement the [OCI runtime spec](https://github.com/opencontainers/runtime-spec/releases/download/v1.0.2/oci-runtime-spec-v1.0.2.html)
* `container process`  the process that starts and runs the container using liblxc (lxcri-start)
* `container config` the LXC config file
* `bundle config` the lxcri container state (bundle path, pidfile ....)
* `runtime spec` the OCI runtime spec from the bundle

## Setup 

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
#LXCRI_LIBEXEC=
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

### Debugging

Apart from the logfile following resources are useful:

* Systemd journal for cri-o and kubelet services
* `coredumpctl` if runtime or container process segfaults.
