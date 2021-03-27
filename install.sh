#!/bin/sh -eux
# -e abort if subshell command exits non-zero
# -u treat undefined variables as error
# -x trace shell expansion

# Package manager dependencies
# NOTE sort lists with: $(echo $PKGS | tr  ' ' '\n' | sort | uniq | xargs)

DISTRIBUTION="$(cat /etc/os-release | grep '^ID=' | cut -d'=' -f2 | tr -d '\n')"
INSTALL_PREFIX=${INSTALL_PREFIX:-/usr/local}
TMPDIR=${TMPDIR:-/tmp/lxcri-build}

case "$DISTRIBUTION" in
"debian" | "ubuntu")
	INSTALL_PKGS=apt_install
	CLEAN_PKGS=apt_clean

	export DEBIAN_FRONTEND=noninteractive

	PKGS_BUILD="automake build-essential ca-certificates git libc6-dev libtool make musl musl-tools pkg-config wget"
	PKGS_BUILD="$PKGS_BUILD libapparmor-dev libbtrfs-dev libc6-dev libcap-dev libdevmapper-dev libglib2.0-dev libseccomp-dev"

	PKGS_RUNTIME="libapparmor1 libbtrfs0 libcap2 libdevmapper1.02.1 libseccomp2"
	PKGS="conntrack ebtables ethtool iproute2 iptables socat"
	PKGS="$PKGS ca-certificates libglib2.0-0 systemd tzdata"
	PKGS="$PKGS $PKGS_RUNTIME"
	;;
"arch")
	INSTALL_PKGS=pacman_install
	CLEAN_PKGS=pacman_clean

	BUILD_PKGS=""
	BUILD_PKGS="$PKGS_PKGS "

	PKGS_RUNTIME=""
	PKGS=""
	PKGS="$PKGS "
	PKGS="$PKGS $PKGS_RUNTIME"
	;;
"alpine")
	INSTALL_PKGS=apk_install
	CLEAN_PKGS=apk_clean

	PKGS_BUILD="build-base wget git libtool m4 automake autoconf"
	PKGS_BUILD="$PKGS_BUILD btrfs-progs-dev glib-dev libseccomp-dev libcap-dev libapparmor-dev"

	PKGS_RUNTIME="libapparmor btrfs-progs libcap lvm2-dev libseccomp libc6-compat libgcc"
	PKGS="conntrack-tools ebtables ethtool iproute2 iptables ip6tables socat"
	PKGS="$PKGS ca-certificates glib runit tzdata"
	PKGS="$PKGS $PKGS_RUNTIME"

	export MUSL_CC="cc"
	;;
*)
	echo "unsupported distribution '$DISTRIBUTION'"
	exit 1
	;;
esac

mkdir -p $TMPDIR
export PATH=${INSTALL_PREFIX}/go/bin:$PATH

setup() {
	$INSTALL_PKGS $@
	add_golang
}

clean() {
	$CLEAN_PKGS $PKGS_BUILD
	remove_golang
	rm -rf $TMPDIR
}

apt_install() {
	apt-get update
	apt-get install -qq --no-install-recommends --yes $@
}

apt_clean() {
	apt-get purge -qq --yes $@
	apt-get autoremove -qq --yes
	apt-get clean -qq
	rm -rf /var/lib/apt/lists/*
}

pacman_install() {
	echo "not implemented"
	exit 1
}

pacman_clean() {
	echo "not implemented"
	exit 1
}

apk_install() {
	echo http://nl.alpinelinux.org/alpine/edge/testing >>/etc/apk/repositories
	echo http://nl.alpinelinux.org/alpine/edge/community >>/etc/apk/repositories
	apk add --no-cache --update $@
}

apk_clean() {
	apk del $@
}

ldconfig_add() {
	if $(which ldconfig 1>/dev/null 2>&1); then
		echo $1 >>/etc/ld.so.conf.d/local.conf
		ldconfig
	fi
	# alpine uses musl libc
	# /etc/ld-musl-x86_64.path (shared library search path, with components delimited by newlines or colons)
	#  default "/lib:/usr/local/lib:/usr/lib"
	# see  musl-libc.org/doc/1.0.0/manual.html
}

add_golang() {
	local src=$GOLANG_SRC
	local checksum=$GOLANG_CHECKSUM
	local archive="$(basename $src)"

	cd ${INSTALL_PREFIX}
	wget --quiet $src
	echo "$checksum  $archive" | sha256sum -c
	tar -xzf $archive
	rm ${INSTALL_PREFIX}/$archive
}

remove_golang() {
	rm -rf $(go env GOPATH)
	rm -rf $(go env GOCACHE)
	rm -rf $(go env GOROOT)
}

git_clone() {
	local tmpdir=$1
	local repo=$2
	local version=$3

	git clone $repo $tmpdir
	cd $tmpdir
	git reset --hard $version
}

add_cni() {
	local repo=$CNI_PLUGINS_GIT_REPO
	local version=$CNI_PLUGINS_GIT_VERSION
	local tmpdir=${TMPDIR}/cni-plugins

	git_clone $tmpdir $repo $version

	./build_linux.sh
	export CNI_PLUGIN_DIR=$INSTALL_PREFIX/cni/bin
	mkdir -p $CNI_PLUGIN_DIR
	cp bin/* $CNI_PLUGIN_DIR

	cd /
	rm -rf $tmpdir
}

add_conmon() {
	local repo=$CONMON_GIT_REPO
	local version=$CONMON_GIT_VERSION
	local tmpdir=${TMPDIR}/conmon

	git_clone $tmpdir $repo $version

	make clean
	make install

	cd /
	rm -rf $tmpdir
}

add_crio() {
	local repo=$CRIO_GIT_REPO
	local version=$CRIO_GIT_VERSION
	local tmpdir=${TMPDIR}/cri-o

	git_clone $tmpdir $repo $version

	make install

	cd /
	rm -rf $tmpdir

	# Modify systemd service file to run with full privileges.
	# This is required for the runtime to set cgroupv2 device controller eBPF.
	sed -i 's/ExecStart=\//ExecStart=+\//' ${INSTALL_PREFIX}/lib/systemd/system/crio.service

	# TODO modify defaults file
}

add_crictl() {
	local checksum=$CRICTL_CHECKSUM
	local url=$CRICTL_URL
	local archive="$(basename $CRICTL_URL)"

	cd ${TMPDIR}
	wget --quiet $url
	echo "$checksum  $archive" | sha256sum -c
	tar -x -z -f $archive -C ${INSTALL_PREFIX}/bin
	rm $archive
}

add_kubernetes() {
	local checksum=$K8S_CHECKSUM
	local url=$K8S_URL
	local archive=$(basename $K8S_URL)

	cd ${TMPDIR}
	wget --quiet $url
	echo "$checksum  $archive" | sha512sum -c
	tar -x -z -f $archive -C $INSTALL_PREFIX/bin --strip-components=3 \
		kubernetes/server/bin/kubectl kubernetes/server/bin/kubeadm kubernetes/server/bin/kubelet
	rm $archive
}

LXC_INSTALL_TOOLS=${LXC_INSTALL_TOOLS:-no}
LXC_INSTALL_COMMANDS=${LXC_INSTALL_COMMANDS:-no}
LXC_INSTALL_DOC=${LXC_INSTALL_DOC:-no}
LXC_INSTALL_API_DOCS=${LXC_INSTALL_API_DOCS:-no}

add_lxc() {
	local repo=$LXC_GIT_REPO
	local version=$LXC_GIT_VERSION
	local tmpdir=${TMPDIR}/lxc

	git_clone $tmpdir $repo $version

	./autogen.sh
	./configure --enable-bash=no --enable-seccomp=yes \
		--enable-capabilities=yes --enable-apparmor=yes \
		--enable-tools=$LXC_INSTALL_TOOLS --enable-commands=$LXC_INSTALL_COMMANDS \
		--enable-static=no --enable-examples=no \
		--enable-doc=$LXC_INSTALL_DOC --enable-api-docs=$LXC_INSTALL_API_DOCS
	make install
	git describe --tags >${INSTALL_PREFIX}/lib/liblxc.version.txt

	ldconfig_add ${INSTALL_PREFIX}/lib
	cd
	rm -rf $tmpdir
}

add_lxcri() {
	local repo=$LXCRI_GIT_REPO
	local version=$LXCRI_GIT_VERSION
	local tmpdir=${TMPDIR}/lxcri

	git_clone $tmpdir $repo $version

	# lxc installed from source with default installation prefix is prefered
	export PKG_CONFIG_PATH=${INSTALL_PREFIX}/lib/pkgconfig
	make install

	cd
	rm -rf $tmpdir
}

install_all_noclean() {
	setup $PKGS_BUILD $PKGS
	add_crictl
	add_kubernetes
	add_cni
	add_conmon
	add_crio
	add_lxc
	add_lxcri
}

install_all() {
	install_all_noclean
	clean
}

install_runtime_noclean() {
	setup $PKGS_BUILD $PKGS_RUNTIME
	add_lxc
	add_lxcri
}

install_runtime() {
	install_runtime_noclean
	clean
}

update_runtime() {
	add_lxc
	add_lxcri
	clean
}

update_lxcri() {
	add_lxcri
	clean
}

$@
