LXC_PPA=${LXC_PPA:-http://ppa.launchpad.net/ubuntu-lxc/lxc-git-master/ubuntu}
LXC_PPA_KEY=${LXC_PPA_KEY:-93763AC528C8C52568951BE0D5495F657635B973}
LXC_PPA_KEYURL="${LXC_PPA_KEYURL:-http://keyserver.ubuntu.com/pks/lookup?op=get&search=0x$LXC_PPA_KEY}"
LXC_PPA_DEPS="curl gnupg2 ca-certificates"

install_lxc_ppa() {
	curl -sSL "$LXC_PPA_KEYURL" | apt-key add - >/dev/null
	echo "deb $LXC_PPA $UBUNTU_CODENAME main" >/etc/apt/sources.list.d/lxc-git-master.list
	apt-get update
	apt_install lxc
}
