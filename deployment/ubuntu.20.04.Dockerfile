FROM ubuntu:20.04

LABEL distribution=ubuntu lxc_from=git lxc_version=master

ENV LXC_INSTALL_FROM=git
ENV LXC_GIT_VERSION=35a68d6df2c240b6604625bd34979ba64db25de7

COPY ubuntu-install-lxc.sh utils.sh /tmp
RUN /tmp/ubuntu-install-lxc.sh
RUN rm /tmp/ubuntu-install-lxc.sh /tmp/utils.sh
