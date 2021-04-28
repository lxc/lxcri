FROM ubuntu:latest
ARG installcmd=install_all

#ENV PKGS="psmisc util-linux"

ENV GOLANG_SRC=https://golang.org/dl/go1.16.3.linux-amd64.tar.gz
ENV GOLANG_CHECKSUM=951a3c7c6ce4e56ad883f97d9db74d3d6d80d5fec77455c6ada6c1f7ac4776d2

ENV CNI_PLUGINS_GIT_REPO=https://github.com/containernetworking/plugins.git
ENV CNI_PLUGINS_GIT_VERSION=v0.9.1

ENV CONMON_GIT_REPO=https://github.com/containers/conmon.git
ENV CONMON_GIT_VERSION=v2.0.27

ENV CRIO_GIT_REPO=https://github.com/cri-o/cri-o.git
ENV CRIO_GIT_VERSION=v1.20.2

ENV CRICTL_CHECKSUM=44d5f550ef3f41f9b53155906e0229ffdbee4b19452b4df540265e29572b899c
ENV CRICTL_URL="https://github.com/kubernetes-sigs/cri-tools/releases/download/v1.20.0/crictl-v1.20.0-linux-amd64.tar.gz"

# see https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG/CHANGELOG-1.20.md
ENV K8S_CHECKSUM=ac936e05aef7bb887a5fb57d50f8c384ee395b5f34c85e5c0effd8709db042359f63247d4a6ae2c0831fe019cd3029465377117e42fff1b00a8e4b7473b88db9
ENV K8S_URL="https://dl.k8s.io/v1.20.6/kubernetes-server-linux-amd64.tar.gz"

## development
ENV LXC_GIT_REPO=https://github.com/lxc/lxc.git
ENV LXC_GIT_VERSION=b9f3cd48ecfed02e4218b55ea1b46273e429a083

ENV LXCRI_GIT_REPO=https://github.com/lxc/lxcri.git
ENV LXCRI_GIT_VERSION=main

COPY install.sh /
RUN /install.sh ${installcmd}
