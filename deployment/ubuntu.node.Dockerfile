FROM localhost/criolxc:0.1

COPY install-node.sh utils.sh /tmp
RUN /tmp/install-node.sh
RUN rm /tmp/install-node.sh /tmp/utils.sh