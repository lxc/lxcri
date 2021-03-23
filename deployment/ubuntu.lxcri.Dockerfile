FROM lxc:0.1

ENV LXCRI_GIT_VERSION=origin/master

COPY install-lxcri.sh utils.sh /tmp
RUN /tmp/install-lxcri.sh
RUN rm /tmp/install-lxcri.sh /tmp/utils.sh
