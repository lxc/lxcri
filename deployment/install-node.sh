#!/bin/sh
# enable debug logging
set -x
# abort if subshell command exits non-zero
set -e

. $(dirname $(readlink -f $0))/utils.sh
CRIO_LXC_BUILD_DEPS="musl musl-tools libc6-dev pkg-config git wget make ca-certificates"

install_cni() {
	local repo="${CNI_PLUGINS_GIT_REPO:-https://github.com/containernetworking/plugins.git}"
	local version="${CNI_PLUGINS_GIT_VERSION:-v0.9.1}"
	local tmpdir=/tmp/cni-plugins

	git clone $repo $tmpdir
	cd $tmpdir
	git reset --hard $version

	./build_linux.sh
	export CNI_PLUGIN_DIR=/usr/local/cni/bin
	mkdir -p $CNI_PLUGIN_DIR
	cp bin/* $CNI_PLUGIN_DIR

	cd /
	rm -rf $tmpdir
}

install_conmon() {
	local repo="${CONMON_GIT_REPO:-https://github.com/containers/conmon.git}"
	local version="${CONMON_GIT_VERSION:-v2.0.27}"
	local tmpdir=/tmp/conmon

	git clone $repo $tmpdir
	cd $tmpdir
	git reset --hard $version

	make clean
	make install

	cd /
	rm -rf $tmpdir
}

install_crio() {
	local repo="${CRIO_GIT_REPO:-https://github.com/cri-o/cri-o.git}"
	local version="${CRIO_GIT_VERSION:-v1.20.1}"

	local tmpdir=/tmp/cri-o
	git clone $repo $tmpdir
	cd $tmpdir
	git reset --hard $version

	make install

	cd /
	rm -rf $tmpdir

	# configure cri-o
	PREFIX=/usr/local
	CRIO_LXC_ROOT=/run/crio-lxc
	# environment for `crio config`
	export CONTAINER_CONMON=${PREFIX}/bin/conmon
	export CONTAINER_PINNS_PATH=${PREFIX}/bin/pinns
	export CONTAINER_DEFAULT_RUNTIME=crio-lxc
	export CONTAINER_RUNTIMES=crio-lxc:${PREFIX}/bin/crio-lxc:$CRIO_LXC_ROOT
	export CONTAINER_CNI_PLUGIN_DIR=$CNI_PLUGIN_DIR

	crio config >/etc/crio/crio.conf

	# Modify systemd service file to run with full privileges.
	# This is required for the runtime to set cgroupv2 device controller eBPF.
	sed -i 's/ExecStart=\//ExecStart=+\//' /usr/local/lib/systemd/system/crio.service
}

install_cri_tools() {
	local release="${CRI_TOOLS_RELEASE:-v1.20.0}"
	local checksum="${CRI_TOOLS_CHECKSUM:-44d5f550ef3f41f9b53155906e0229ffdbee4b19452b4df540265e29572b899c}"
	local arch="linux-amd64"
	local archive="crictl-${release}-${arch}.tar.gz"
	local url="https://github.com/kubernetes-sigs/cri-tools/releases/download/$release/$archive"
	local destdir="/usr/local/bin"

	cd /tmp
	wget --quiet $url
	echo "$checksum $archive" | sha256sum -c
	tar -x -z -f $archive -C $destdir
	rm $archive
}

install_kubernetes() {
	# see https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG/CHANGELOG-1.20.md
	local checksum="${K8S_CHECKSUM:-37738bc8430b0832f32c6d13cdd68c376417270568cd9b31a1ff37e96cfebcc1e2970c72bed588f626e35ed8273671c77200f0d164e67809b5626a2a99e3c5f5}"
	local release="${K8S_RELEASE:-v1.20.4}"
	local arch="linux-amd64"
	# TODO maybe make arch a global variable ${GOHOSTOS}-${GOHOSTARCH}
	local archive="kubernetes-server-$arch.tar.gz"
	local url="https://dl.k8s.io/${release}/${archive}"
	local destdir="/usr/local/bin"

	cd /tmp
	wget --quiet $url
	echo "$checksum $archive" | sha512sum -c
	tar -x -z -f $archive -C $destdir --strip-components=3 \
		kubernetes/server/bin/kubectl kubernetes/server/bin/kubeadm kubernetes/server/bin/kubelet
	rm $archive

	cat >/etc/systemd/system/kubelet.service <<-EOF
		[Unit]
		Description=kubelet: The Kubernetes Node Agent
		Documentation=http://kubernetes.io/docs/

		[Service]
		ExecStart=$destdir/kubelet
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
		ExecStart=$destdir/kubelet \$KUBELET_KUBECONFIG_ARGS \$KUBELET_CONFIG_ARGS \$KUBELET_KUBEADM_ARGS \$KUBELET_EXTRA_ARGS
	EOF

	#systemctl daemon-reload
	systemctl enable kubelet.service
}

# TODO let install functions append build dependencies and runtime dependencies
BUILD_DEPS_CONMON="libglib2.0-dev"
BUILD_DEPS_CRIO="libseccomp-dev libapparmor-dev libbtrfs-dev libdevmapper-dev libcap-dev libc6-dev"
BUILD_DEPS="wget ca-certificates git build-essential libtool make automake pkg-config"
BUILD_DEPS="${BUILD_DEPS} ${BUILD_DEPS_CONMON} ${BUILD_DEPS_CRIO}"

DEPS_CONMON="libglib2.0-0"
DEPS_CRIO="tzdata"
DEPS_K8S="ebtables ethtool socat conntrack iproute2 iptables"
DEPS="$DEPS_CONMON $DEPS_CRIO $DEPS_K8S"
DEPS="$DEPS systemd"

PKGS="$DEPS $BUILD_DEPS"

apt_install $PKGS
install_golang
install_cni
install_conmon
install_cri_tools
install_crio
install_kubernetes
uninstall_golang
apt_clean $BUILD_DEPS
