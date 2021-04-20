# About

`lxcri` is a wrapper around [LXC](https://github.com/lxc/lxc) which can be used as
a drop-in container runtime replacement for use by [CRI-O](https://github.com/kubernetes-sigs/cri-o).

### OCI compliance

With liblxc >= https://github.com/lxc/lxc/commit/b5daeddc5afce1cad4915aef3e71fdfe0f428709
it passes all sonobuoy conformance tests.

## Installation

For the installation of the runtime see [install.md](doc/install.md)</br>
For the installation and initialization of a kubernetes cluster see [kubernetes.md](doc/kubernetes.md)

## Bugs

* cli: --help shows environment values not defaults https://github.com/urfave/cli/issues/1206

## Requirements and restrictions

* Only cgroupv2 (unified cgroup hierarchy) is supported.
* A recent kernel >= 5.8 is required for full cgroup support.

### Unimplemented features

* [runtime: Implement POSIX platform hooks](https://github.com/Drachenfels-GmbH/lxcri/issues/10)
* [runtime: Implement cgroup2 resource limits](https://github.com/Drachenfels-GmbH/lxcri/issues/11)
