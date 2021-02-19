#!/bin/sh
# enable debug logging
set -x
# abort if subshell command exits non-zero
set -e

export DEBIAN_FRONTEND=noninteractive

apt_cleanup() {
	apt-get purge --yes $@
	apt-get autoremove --yes
	apt-get clean
	rm -rf /var/lib/apt/lists/*
}

apt_install() {
	apt-get update
	apt-get install --no-install-recommends --yes $@
}

CRIO_LXC_GIT_REPO="${CRIO_LXC_GIT_REPO:-https://github.com/Drachenfels-GmbH/crio-lxc.git}"
CRIO_LXC_GIT_VERSION="${CRIO_LXC_GIT_VERSION:-master}"
CRIO_LXC_BUILD_DEPS="musl musl-tools libc6-dev  pkg-config git wget make ca-certificates"

GOLANG_SRC="${GOLANG_SRC:-https://golang.org/dl/go1.16.linux-amd64.tar.gz}"
GOLANG_CHECKSUM="${GOLANG_CHECKSUM:-013a489ebb3e24ef3d915abe5b94c3286c070dfe0818d5bca8108f1d6e8440d2}"

install_golang() {
	local archive="$(basename $GOLANG_SRC)"
	cd /tmp
	wget --quiet $GOLANG_SRC
	echo "$GOLANG_CHECKSUM $archive" | sha256sum -c
	tar -C /usr/local -xzf $archive
	export PATH=/usr/local/go/bin:$PATH
	rm /tmp/$archive
}

uninstall_golang() {
	rm -rf $(go env GOPATH)
	rm -rf $(go env GOCACHE)
	rm -rf $(go env GOROOT)
}

install_crio_lxc() {
	local tmpdir=/tmp/lxc
	git clone $CRIO_LXC_GIT_REPO $tmpdir
	cd $tmpdir
	git reset --hard $CIO_LXC_GIT_VERSION
	# lxc installed from source with dafault installation prefix is prefered
	export PKG_CONFIG_PATH=/usr/local/lib/pkgconfig:$PKG_CONFIG_PATH
	make install
	cd
	rm -rf $tmpdir
}

apt_install $CRIO_LXC_BUILD_DEPS
install_golang
install_crio_lxc
uninstall_golang
apt_cleanup $CRIO_LXC_BUILD_DEPS
