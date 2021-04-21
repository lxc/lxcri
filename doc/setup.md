# Setup

NOTE: This documentation is not yet complete and will be updated.

## cgroups

Enable cgroupv2 unified hierarchy manually:

`mount -t cgroup2 none /sys/fs/cgroup`

or permanent via kernel cmdline params:

`systemd.unified_cgroup_hierarchy=1 cgroup_no_v1=all`

## cri-o

```
PREFIX=/usr/local
LXCRI_ROOT=/run/lxcri

# environment for `crio config`
export CONTAINER_CONMON=${PREFIX}/bin/conmon
export CONTAINER_PINNS_PATH=${PREFIX}/bin/pinns
export CONTAINER_DEFAULT_RUNTIME=lxcri
export CONTAINER_RUNTIMES=lxcri:${PREFIX}/bin/lxcri:$LXCRI_ROOT

crio config > /etc/crio/crio.conf
```

### cgroupv2 ebpf

Modify systemd service file to run with full privileges.</br>
This is required for the runtime to set cgroupv2 device controller eBPF.</br>
See https://github.com/cri-o/cri-o/pull/4272

```
sed -i 's/ExecStart=\//ExecStart=+\//' /usr/local/lib/systemd/system/crio.service
systemctl daemon-reload
systemctl start crio
```

### HTTP proxy

If you need a HTTP proxy for internet access you may have to set the proxy environment variables in `/etc/default/crio`
for crio-o to be able to fetch images from remote repositories.

```
http_proxy="http://myproxy:3128"
https_proxy="http://myproxy:3128"
no_proxy="10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.0/8,127.0.0.1,localhost"
```

## /etc/containers

### storage

If you're using `overlay` as storage driver cri-o may complain that it is not using `native diff` mode.</br>
Update `/etc/containers/storage.conf` to fix this.

```
# see https://github.com/containers/storage/blob/v1.20.2/docs/containers-storage.conf.5.md
[storage]
driver = "overlay"

[storage.options.overlay]
# see https://www.kernel.org/doc/Documentation/filesystems/overlayfs.txt, `modinfo overlay`
# [ 8270.526807] overlayfs: conflicting options: metacopy=on,redirect_dir=off
# NOTE: metacopy can only be enabled when redirect_dir is enabled
# NOTE: storage driver name must be set or mountopt are not evaluated,
# even when the driver is the default driver --> BUG ?
mountopt = "nodev,redirect_dir=off,metacopy=off"
```
