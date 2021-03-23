#!/bin/sh
# enable debug logging
set -x
# abort if subshell command exits non-zero
set -e

. $(dirname $(readlink -f $0))/utils.sh

LXCRI_GIT_REPO="${LXCRI_GIT_REPO:-https://github.com/drachenfels-de/lxcri.git}"
LXCRI_GIT_VERSION="${LXCRI_GIT_VERSION:-master}"
LXCRI_BUILD_DEPS="musl musl-tools libc6-dev pkg-config git wget make ca-certificates"

install_crio_lxc() {
	local tmpdir=/tmp/lxc
	git clone $LXCRI_GIT_REPO $tmpdir
	cd $tmpdir
	git reset --hard $LXCRI_GIT_VERSION
	# lxc installed from source with dafault installation prefix is prefered
	export PKG_CONFIG_PATH=/usr/local/lib/pkgconfig:$PKG_CONFIG_PATH
	make install
	cd
	rm -rf $tmpdir
}

apt_install $LXCRI_BUILD_DEPS
install_golang
install_crio_lxc
uninstall_golang
apt_clean $LXCRI_BUILD_DEPS
