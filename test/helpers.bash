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
        -e "s,CRIO_REPO,$CRIO_REPO,g" \
        "$ROOT_DIR/test/crio.conf.in" > "$TEMP_DIR/crio.conf"
    # it doesn't like seccomp_profile = "", so let's make a bogus one
    echo "{}" > "$TEMP_DIR/seccomp.json"
    # it doesn't like if these dirs don't exist, so make them
    mkdir -p "$TEMP_DIR/cni"
    mkdir -p "$TEMP_DIR/cni-plugins"
    # set up an insecure policy
    echo '{"default": [{"type": "insecureAcceptAnything"}]}' > "$TEMP_DIR/policy.json"
    "$CRIO_REPO/bin/crio" --config "$TEMP_DIR/crio.conf" &
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
    rm -rf "$TEMP_DIR" || true
}

function crictl {
    # watch out for: https://github.com/kubernetes-sigs/cri-tools/issues/460
    $(which crictl) --runtime-endpoint "$TEMP_DIR/crio.sock" $@
    echo "$output"
}

function crio-lxc {
    $ROOT_DIR/crio-lxc --lxc-path "$TEMP_DIR/lxcpath" $@
    echo "$output"
}
