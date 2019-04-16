load helpers

function setup() {
    make_tempdir
    skopeo --insecure-policy copy docker://centos:latest oci:$ROOT_DIR/test/oci-cache:centos
    umoci unpack --image "$ROOT_DIR/test/oci-cache:centos" "$TEMP_DIR/dest"
    sed -i -e "s?rootfs?$TEMP_DIR/dest/rootfs?" "$TEMP_DIR/dest/config.json"
}

function teardown() {
    cleanup_tempdir
}

@test "manual invocation" {
    crio-lxc --debug --log-level trace --log-file "$TEMP_DIR/log" create --bundle "$TEMP_DIR/dest" centos
    crio-lxc --debug --log-level trace --log-file "$TEMP_DIR/log" start centos
}
