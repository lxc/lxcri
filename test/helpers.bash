ROOT_DIR=$(git rev-parse --show-toplevel)

function make_tempdir {
    declare -g TEMP_DIR=$(realpath $(mktemp -d crio-lxc-test.XXXXXXXX))
    # not strictly necessary, but nice if we end up debugging things by keeping
    # the tempdir around
    chmod 755 "$TEMP_DIR"
}

function setup_crio {
    make_tempdir
    sed \
        -e "s,CRIOLXC_TEST_DIR,$TEMP_DIR,g" \
        -e "s,CRIOLXC_BINARY,$ROOT_DIR/crio-lxc,g" \
        -e "s,PACKAGES_DIR,$PACKAGES_DIR,g" \
        "$ROOT_DIR/test/crio.conf.in" > "$TEMP_DIR/crio.conf"
    # it doesn't like seccomp_profile = "", so let's make a bogus one
    echo "{}" > "$TEMP_DIR/seccomp.json"
    # It doesn't like if these dirs don't exist, so always them
    # You can't start a pod without them, so if you're going to test
    # basic.bats, then
    #    cd ~/packages;  git clone https://github.com/containernetworking/cni
    #    git clone https://github.com/containernetworking/plugins cni-plugins
    #    cd cni-plugins; ./build_linux.sh
    mkdir -p "$TEMP_DIR/cni/net.d"
    mkdir -p /tmp/busybox # for the logfile as per log_directory in test/basic-pod-config.json
    if [ -d ~/packages/cni-plugins ]; then
        rsync -a ~/packages/cni-plugins $TEMP_DIR/
        cat > $TEMP_DIR/cni/net.d/10-myptp.conf << EOF
{"cniVersion":"0.3.1","name":"myptp","type":"ptp","ipMasq":true,"ipam":{"type":"host-local","subnet":"172.16.29.0/24","routes":[{"dst":"0.0.0.0/0"}]}}
EOF
    else
        mkdir -p "$TEMP_DIR/cni-plugins"
    fi
    # set up an insecure policy
    echo '{"default": [{"type": "insecureAcceptAnything"}]}' > "$TEMP_DIR/policy.json"
    "$PACKAGES_DIR/cri-o/bin/crio" --config "$TEMP_DIR/crio.conf" &
    declare -g CRIO_PID=$!
}

function cleanup_crio {
    kill -SIGTERM $CRIO_PID || true
    # wait until it dies; it has a bunch of stuff mounted, and we'll get
    # various EBUSY races if we don't
    wait $CRIO_PID
    cleanup_tempdir
}

function cleanup_tempdir {
    [ -f .keeptempdirs ] || rm -rf "$TEMP_DIR" || true
}

function crictl {
    # watch out for: https://github.com/kubernetes-sigs/cri-tools/issues/460
    # If you need more debug output, set CRICTLDEBUG to -D
    CRICTLDEBUG=""
    $(which crictl) ${CRICTLDEBUG} --runtime-endpoint "$TEMP_DIR/crio.sock" $@
    echo "$output"
}

function crio-lxc {
    $ROOT_DIR/crio-lxc --lxc-path "$TEMP_DIR/lxcpath" $@
    echo "$output"
}
