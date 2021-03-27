FROM ubuntu:latest
ARG installcmd=install_all

#ENV PKGS="psmisc util-linux"

ENV GOLANG_SRC=https://golang.org/dl/go1.16.2.linux-amd64.tar.gz
ENV GOLANG_CHECKSUM=542e936b19542e62679766194364f45141fde55169db2d8d01046555ca9eb4b8

ENV CNI_PLUGINS_GIT_REPO=https://github.com/containernetworking/plugins.git
ENV CNI_PLUGINS_GIT_VERSION=v0.9.1

ENV CONMON_GIT_REPO=https://github.com/containers/conmon.git
ENV CONMON_GIT_VERSION=v2.0.27

ENV CRIO_GIT_REPO=https://github.com/cri-o/cri-o.git
ENV CRIO_GIT_VERSION=v1.20.1

ENV CRICTL_CHECKSUM=44d5f550ef3f41f9b53155906e0229ffdbee4b19452b4df540265e29572b899c
ENV CRICTL_URL="https://github.com/kubernetes-sigs/cri-tools/releases/download/v1.20.0/crictl-v1.20.0-linux-amd64.tar.gz"

# see https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG/CHANGELOG-1.20.md
ENV K8S_CHECKSUM=37738bc8430b0832f32c6d13cdd68c376417270568cd9b31a1ff37e96cfebcc1e2970c72bed588f626e35ed8273671c77200f0d164e67809b5626a2a99e3c5f5
ENV K8S_URL="https://dl.k8s.io/v1.20.4/kubernetes-server-linux-amd64.tar.gz"

## development
ENV LXC_GIT_REPO=https://github.com/lxc/lxc.git
ENV LXC_GIT_VERSION=master

ENV LXCRI_GIT_REPO=https://github.com/drachenfels-de/lxcri.git
ENV LXCRI_GIT_VERSION=main

COPY install.sh /
RUN /install.sh ${installcmd}
