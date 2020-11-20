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

#### change liblxc source path

The installation prefix environment variable is set to `PREFIX=/usr/local` by default.
The library source path for `pkg-config` is set to `$PREFIX/lib/pkg-config` by default.
You can change that by setting the `PKG_CONFIG_PATH` environment variable.

E.g to install binaries in `/opt/bin` and use liblxc from `/usr/lib`:

	PREFIX=/opt PKG_CONFIG_PATH=/usr/lib/pkgconfig make install

#### rebuild all libraries

To rebuild all libraries set `GOFLAGS=-a`.
E.g after an liblxc upgrade the go-lxc library must be rebuild.

	make clean
	GOFLAGS=-a make

### cri-o

crio-lxc-install 

## Notes

Note that you must have a new enough liblxc, one which supports the
"lxc.rootfs.managed" key.  3.0.3 is not new enough, 3.1 is.  On Ubuntu,
you can upgrade using the ubuntu-lxc/lxc-git-master PPA.  Arch and
OpenSUSE tumbleweed should be uptodate.

## Tests

To run the 'basic' test, you'll need to build cri-o and CNI.

```
mkdir ~/packages
cd packages
git clone https://github.com/kubernetes-sigs/cri-o
cd cri-o
make
cd ..
git clone https://github.com/containernetworking/cni
git clone https://github.com/containernetworking/plugins cni-plugins
cd cni-plugins
./build_linux.sh
```

You'll also need crictl.  Download the tarball, extract it, and
copy crictl to somewhere in your path:

```
wget https://github.com/kubernetes-sigs/cri-tools/releases/download/v1.14.0/crictl-v1.14.0-linux-amd64.tar.gz
tar zxf crictl-v1.14.0-linux-amd64.tar.gz
sudo cp crictl /usr/local/bin # or ~/.local/bin, etc.
```

You'll also need conntrack installed:

```
apt install conntrack
```

You have to install [bats](https://github.com/bats-core/bats-core) to run the tests.
On debian you can install bats with:
	
	apt-get install bats


To keep the tempdir of of a test run, you have to create the lockfile `.keeptempdirs` 
in the top-level of this repository.

	touch .keeptempdirs

To debug `crictl` calls within a test run:

	CRICTLDEBUG="-D" make basic.bats

`bats` does not show any output when the test was successful.
The logfile is created in /tmp and deleted when the test was successful.
To watch the test output while the test is running:

	tail -f /tmp/bats.*.log

Expand multi-line output int cri-o log (e.g stacktrace)

echo "output" | sed 's/\\n\\t/\n/g' 


# TODO
## generate nice buttons for the project page
https://goreportcard.com
