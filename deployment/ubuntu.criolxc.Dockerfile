FROM myimage

ENV CRIO_LXC_GIT_VERSION=master

COPY install-crio-lxc.sh utils.sh /tmp
RUN /tmp/install-crio-lxc.sh
RUN rm /tmp/install-crio-lxc.sh /tmp/utils.sh
