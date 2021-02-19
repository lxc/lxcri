FROM debian-buster-lxc:0.1

COPY install-crio-lxc.sh /tmp
RUN /tmp/install-crio-lxc.sh
RUN rm /tmp/install-crio-lxc.sh
