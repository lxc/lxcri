# crio-lxc

This is a wrapper around [LXC](https://github.com/lxc/lxc) which can be used as
a drop-in container runtime replacement for use by
[CRI-O](https://github.com/kubernetes-sigs/cri-o).

To use this, simply build it:

```
make
```

Then specify the `crio-lxc` binary you just built as the value for
`default_runtime` in the `crio.runtime` section of `/etc/crio/crio.conf`.
