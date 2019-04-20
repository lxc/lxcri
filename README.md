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

## Notes

Note that you must have a new enough liblxc, one which supports the
"lxc.rootfs.managed" key.  3.0.3 is not new enough, 3.1 is.  On Ubuntu,
you can upgrade using the ubuntu-lxc/lxc-git-master PPA.  Arch and
OpenSUSE tumbleweed should be uptodate.

## Tests

To run the 'basic' test, you'll need to build cri-o.

```
mkdir ~/packages
cd packages
git clone https://github.com/kubernetes-sigs/cri-o
cd cri-o
make
```

You'll also need crictl.  Download the tarball, extract it, and
copy crictl to /usr/bin:

```
wget https://github.com/kubernetes-sigs/cri-tools/releases/download/v1.13.0/crictl-v1.14.0-linux-amd64.tar.gz
tar zxf crictl-v1.14.0-linux-amd64.tar.gz
sudo cp crictl /usr/bin
```

You'll also need conntrack installed:

```
apt install conntrack
```
