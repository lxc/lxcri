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

add_golang() {
	local src=$GOLANG_SRC
	local checksum=$GOLANG_CHECKSUM
	local archive="$(basename $src)"

	cd ${INSTALL_PREFIX}
	wget --quiet $src
	echo "$checksum $archive" | sha256sum -c
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
	echo "$checksum $archive" | sha256sum -c
	tar -x -z -f $archive -C ${INSTALL_PREFIX}/bin
	rm $archive
}

add_kubernetes() {
	local checksum=$K8S_CHECKSUM
	local url=$K8S_URL
	local archive=$(basename $K8S_URL)

	cd ${TMPDIR}
	wget --quiet $url
	echo "$checksum $archive" | sha512sum -c
	tar -x -z -f $archive -C $INSTALL_PREFIX/bin --strip-components=3 \
		kubernetes/server/bin/kubectl kubernetes/server/bin/kubeadm kubernetes/server/bin/kubelet
	rm $archive

	cat >/etc/systemd/system/kubelet.service <<-EOF
		[Unit]
		Description=kubelet: The Kubernetes Node Agent
		Documentation=http://kubernetes.io/docs/

		[Service]
		ExecStart=${INSTALL_PREFIX}/kubelet
		Restart=always
		StartLimitInterval=0
		RestartSec=10

		[Install]
		WantedBy=multi-user.target
	EOF

	mkdir -p /etc/systemd/system/kubelet.service.d
	cat >/etc/systemd/system/kubelet.service.d/10-kubeadm.conf <<-EOF
		[Service]
		Environment="KUBELET_KUBECONFIG_ARGS=--bootstrap-kubeconfig=/etc/kubernetes/bootstrap-kubelet.conf --kubeconfig=/etc/kubernetes/kubelet.conf"
		Environment="KUBELET_CONFIG_ARGS=--config=/var/lib/kubelet/config.yaml"
		# This is a file that "kubeadm init" and "kubeadm join" generate at runtime, populating the KUBELET_KUBEADM_ARGS variable dynamically
		EnvironmentFile=-/var/lib/kubelet/kubeadm-flags.env
		# This is a file that the user can use for overrides of the kubelet args as a last resort. Preferably, the user should use
		# the .NodeRegistration.KubeletExtraArgs object in the configuration files instead. KUBELET_EXTRA_ARGS should be sourced from this file.
		EnvironmentFile=-/etc/default/kubelet
		ExecStart=
		ExecStart=${INSTALL_PREFIX}/kubelet \$KUBELET_KUBECONFIG_ARGS \$KUBELET_CONFIG_ARGS \$KUBELET_KUBEADM_ARGS \$KUBELET_EXTRA_ARGS
	EOF

	#systemctl daemon-reload
	systemctl enable kubelet.service
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
	echo ${INSTALL_PREFIX}/lib >>/etc/ld.so.conf.d/local.conf
	ldconfig

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

configure_runtime() {
	# TODO configure runtime using 'lxcri {flags} config'
	CRIO_LXC_ROOT=/run/lxcri
	# configure cri-o
	# environment for `crio config`
	export CONTAINER_CONMON=${INSTALL_PREFIX}/bin/conmon
	export CONTAINER_PINNS_PATH=${INSTALL_PREFIX}/bin/pinns
	export CONTAINER_DEFAULT_RUNTIME=lxcri
	export CONTAINER_RUNTIMES=lxcri:${INSTALL_PREFIX}/bin/lxcri:$CRIO_LXC_ROOT
	export CONTAINER_CNI_PLUGIN_DIR=$CNI_PLUGIN_DIR

	crio config >/etc/crio/crio.conf
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
