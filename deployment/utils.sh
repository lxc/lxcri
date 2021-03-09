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

GOLANG_SRC="${GOLANG_SRC:-https://golang.org/dl/go1.16.linux-amd64.tar.gz}"
GOLANG_CHECKSUM="${GOLANG_CHECKSUM:-013a489ebb3e24ef3d915abe5b94c3286c070dfe0818d5bca8108f1d6e8440d2}"

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
