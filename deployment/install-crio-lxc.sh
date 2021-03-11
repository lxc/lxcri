#!/bin/sh
# enable debug logging
set -x
# abort if subshell command exits non-zero
set -e

. $(dirname $(readlink -f $0))/utils.sh

CRIO_LXC_GIT_REPO="${CRIO_LXC_GIT_REPO:-https://github.com/Drachenfels-GmbH/crio-lxc.git}"
CRIO_LXC_GIT_VERSION="${CRIO_LXC_GIT_VERSION:-master}"
CRIO_LXC_BUILD_DEPS="musl musl-tools libc6-dev pkg-config git wget make ca-certificates"

install_crio_lxc() {
	local tmpdir=/tmp/lxc
	git clone $CRIO_LXC_GIT_REPO $tmpdir
	cd $tmpdir
	git reset --hard $CRIO_LXC_GIT_VERSION
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
apt_clean $CRIO_LXC_BUILD_DEPS
