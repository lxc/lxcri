#!/bin/sh
export DEBIAN_FRONTEND=noninteractive

apt_install() {
	apt-get update
	apt-get install --no-install-recommends --yes $@
}

apt_clean() {
	apt-get purge --yes $@
	apt-get autoremove --yes
	apt-get clean
	rm -rf /var/lib/apt/lists/*
}

GOLANG_SRC="${GOLANG_SRC:-https://golang.org/dl/go1.16.1.linux-amd64.tar.gz}"
GOLANG_CHECKSUM="${GOLANG_CHECKSUM:-3edc22f8332231c3ba8be246f184b736b8d28f06ce24f08168d8ecf052549769}"

install_golang() {
	local archive="$(basename $GOLANG_SRC)"
	local destdir="/tmp"

	cd $destdir
	wget --quiet $GOLANG_SRC
	echo "$GOLANG_CHECKSUM $archive" | sha256sum -c
	tar -xzf $archive
	export PATH=$destdir/go/bin:$PATH
	rm $destdir/$archive
}

uninstall_golang() {
	rm -rf $(go env GOPATH)
	rm -rf $(go env GOCACHE)
	rm -rf $(go env GOROOT)
}

LXC_GIT_REPO="${LXC_GIT_REPO:-https://github.com/lxc/lxc.git}"
LXC_GIT_VERSION="${LXC_GIT_VERSION:-master}"

LXC_INSTALL_TOOLS=${LXC_INSTALL_TOOLS:-no}
LXC_INSTALL_COMMANDS=${LXC_INSTALL_COMMANDS:-no}
LXC_INSTALL_DOC=${LXC_INSTALL_DOC:-no}
LXC_INSTALL_API_DOCS=${LXC_INSTALL_API_DOCS:-no}

install_lxc_git() {
	local tmpdir=/tmp/lxc
	git clone $LXC_GIT_REPO $tmpdir
	cd $tmpdir
	git reset --hard $LXC_GIT_VERSION
	./autogen.sh
	./configure --enable-bash=no --enable-seccomp=yes \
		--enable-capabilities=yes --enable-apparmor=yes \
		--enable-tools=$LXC_INSTALL_TOOLS --enable-commands=$LXC_INSTALL_COMMANDS \
		--enable-static=no --enable-examples=no \
		--enable-doc=$LXC_INSTALL_DOC --enable-api-docs=$LXC_INSTALL_API_DOCS
	make install
	git describe --tags >/usr/local/lib/liblxc.version.txt
	echo /usr/local/lib >>/etc/ld.so.conf.d/local.conf
	ldconfig
	cd
	rm -rf $tmpdir
}
