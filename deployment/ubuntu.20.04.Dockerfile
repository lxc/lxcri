FROM ubuntu:20.04

LABEL distribution=ubuntu lxc_from=git lxc_version=master

ENV LXC_INSTALL_FROM=git

COPY ubuntu-install-lxc.sh /tmp
RUN /tmp/ubuntu-install-lxc.sh
RUN rm /tmp/ubuntu-install-lxc.sh
