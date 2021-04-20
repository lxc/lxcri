## cgroups

Enable cgroupv2 unified hierarchy manually:

```
mount -t cgroup2 none /sys/fs/cgroup
```

or permanent via kernel cmdline params:
 
 ```
 systemd.unified_cgroup_hierarchy=1 cgroup_no_v1=all
 ```

## build dependencies

Install the build dependencies which are required to build the runtime and runtime dependencies.

### debian

```sh
# liblxc / conmon build dependencies
apt-get install build-essential libtool automake pkg-config \
libseccomp-dev libapparmor-dev libbtrfs-dev \
libdevmapper-dev libcap-dev libc6-dev libglib2.0-dev
# k8s dependencies, tools
apt-get install jq ebtables iptables conntrack
```

### arch linux

```sh
# liblxc / conmon build dependencies
pacman -Sy base-devel apparmor libseccomp libpcap btrfs-progs
# k8s dependencies
pacman -Sy conntrack-tools ebtables jq
```

## runtime dependencies

* [lxc](https://github.com/lxc/lxc.git) >= b5daeddc5afce1cad4915aef3e71fdfe0f428709
* [conmon/pinns](https://github.com/containers/conmon.git) v2.0.22
* [cri-o](https://github.com/cri-o/cri-o.git) release-1.20

By default everything is installed to `/usr/local`

### lxc (liblxc)

```sh
git clone https://github.com/lxc/lxc.git
cd lxc
./autogen.sh
./configure --enable-bash=no --enable-seccomp=yes \
  --enable-capabilities=yes --enable-apparmor=yes
make install

git describe --tags > /usr/local/lib/liblxc.version.txt
echo /usr/local/lib > /etc/ld.so.conf.d/local.conf
ldconfig
```

### lxcri

```
make install
```

The installation prefix environment variable is set to `PREFIX=/usr/local` by default.</br>
The library source path for `pkg-config` is set to `$PREFIX/lib/pkg-config` by default.</br>
You can change that by setting the `PKG_CONFIG_PATH` environment variable.</br>

E.g to install binaries in `/opt/bin` but use liblxc from `/usr/lib`:

  PREFIX=/opt PKG_CONFIG_PATH=/usr/lib/pkgconfig make install

Keep in mind that you have to change the `INSTALL_PREFIX` in the crio install script below.

### conmon

```sh
git clone https://github.com/containers/conmon.git
cd conmon
git reset --hard v2.0.22
make clean
make install
```

### cri-o

```sh
#!/bin/sh
git clone https://github.com/cri-o/cri-o.git
cd cri-o
git reset --hard origin/release-1.20
make install

PREFIX=/usr/local
CRIO_LXC_ROOT=/run/lxcri

# environment for `crio config`
export CONTAINER_CONMON=${PREFIX}/bin/conmon
export CONTAINER_PINNS_PATH=${PREFIX}/bin/pinns
export CONTAINER_DEFAULT_RUNTIME=lxcri
export CONTAINER_RUNTIMES=lxcri:${PREFIX}/bin/lxcri:$CRIO_LXC_ROOT

crio config > /etc/crio/crio.conf
```

#### cgroupv2 ebpf

Modify systemd service file to run with full privileges.</br>
This is required for the runtime to set cgroupv2 device controller eBPF.</br>
See https://github.com/cri-o/cri-o/pull/4272

```
sed -i 's/ExecStart=\//ExecStart=+\//' /usr/local/lib/systemd/system/crio.service
systemctl daemon-reload
systemctl start crio
```

#### storage configuration

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

#### HTTP proxy

If you need a HTTP proxy for internet access you may have to set the proxy environment variables in `/etc/default/crio`
for crio-o to be able to fetch images from remote repositories.

```
http_proxy="http://myproxy:3128"
https_proxy="http://myproxy:3128"
no_proxy="10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.0/8,127.0.0.1,localhost"
```
