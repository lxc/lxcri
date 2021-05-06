## Glossary

* `runtime` the lxcri binary and the command set that implement the [OCI runtime spec](https://github.com/opencontainers/runtime-spec/releases/download/v1.0.2/oci-runtime-spec-v1.0.2.html)
* `container process`  the process that starts and runs the container using liblxc (lxcri-start)
* `container config` the LXC config file
* `bundle config` the lxcri container state (bundle path, pidfile ....)
* `runtime spec` the OCI runtime spec from the bundle

## Configuration

The runtime binary implements flags that are required by the `OCI runtime spec`,</br>
and flags that are runtime specific (timeouts, hooks, logging ...).

The configuration file path defaults to **/etc/lxcri/lxcri.yaml**
and can changed with the **LXCRI_CONFIG** environment variable.
Most of the runtime specific flags have corresponding environment variables. See `lxcri --help`.</br>
The `lxcri` cli command valuates the configuration in the following order (higher order takes precedence).

1. builtin default configuration
2. configuration file
3. environment variables
4. commandline flags

The `lxcri config` command can be used to display and update the configuration. e.g:

* `lxcri config` print active configuration (builtin default configuration merged with config file)
* `lxcri config --default` shows builtin default configuration
* `lxcri --log-level debug config` print modified configuration
* `lxcri --log-level debug config --update-current` update/create modified configuration

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
