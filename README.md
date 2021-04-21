[![Go Reference](https://pkg.go.dev/badge/github.com/lxc/lxcri.svg)](https://pkg.go.dev/github.com/lxc/lxcri)
![Build](https://github.com/lxc/lxcri/actions/workflows/build.yml/badge.svg)

# About

`lxcri` is a wrapper around [LXC](https://github.com/lxc/lxc) which can be used as
a drop-in container runtime replacement for use by [CRI-O](https://github.com/kubernetes-sigs/cri-o).

### OCI compliance

With liblxc starting from [lxc-4.0.0-927-gb5daeddc5](https://github.com/lxc/lxc/commit/b5daeddc5afce1cad4915aef3e71fdfe0f428709)
it passes all sonobuoy conformance tests.

## Installation

For the installation of the runtime see [install.md](doc/install.md)</br>
For the installation and initialization of a kubernetes cluster see [kubernetes.md](doc/kubernetes.md)

## API Usage

Please have a look at the [runtime tests](runtime_test.go) for now.

## Notes

* It's currently only tested with cgroups v2.
