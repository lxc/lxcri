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
