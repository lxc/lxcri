[![Go Reference](https://pkg.go.dev/badge/github.com/lxc/lxcri.svg)](https://pkg.go.dev/github.com/lxc/lxcri)
![Build](https://github.com/lxc/lxcri/actions/workflows/build.yml/badge.svg)

# About

`lxcri` is a wrapper around [LXC](https://github.com/lxc/lxc) which can be used as
a drop-in container runtime replacement for use by [CRI-O](https://github.com/kubernetes-sigs/cri-o).

### OCI compliance

With liblxc starting from [lxc-4.0.0-927-gb5daeddc5](https://github.com/lxc/lxc/commit/b5daeddc5afce1cad4915aef3e71fdfe0f428709)
it passes all sonobuoy conformance tests.

## Build

You can use the provided [Dockerfile](Dockerfile) to build an</br>

runtime only image (`lxcri` + `lxc`)

`docker build --build-arg installcmd=install_runtime`

or with everything required for a kubernetes node (kubelet, kubeadm, cri-o, lxcri, lxc ...)

`docker build`

Note: The images are not pre-configured and you must follow the steps in setup for now.

## Setup

To use `lxcri` as OCI runtime in `cri-o` see [setup.md](doc/setup.md)

## API Usage

Please have a look at the [runtime tests](runtime_test.go) for now.

## Notes

* It's currently only tested with cgroups v2.
