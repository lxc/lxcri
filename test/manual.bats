load helpers

function setup() {
    make_tempdir
    skopeo --insecure-policy copy docker://alpine:latest oci:$ROOT_DIR/test/oci-cache:alpine
    umoci unpack --image "$ROOT_DIR/test/oci-cache:alpine" "$TEMP_DIR/dest"
    sed -i -e "s?rootfs?$TEMP_DIR/dest/rootfs?" "$TEMP_DIR/dest/config.json"
    sed -i -e "s?\"/bin/sh\"?\"/bin/sleep\",\n\"10\"?" "$TEMP_DIR/dest/config.json"
    sed -i -e "s?\"type\": \"ipc\"?\"type\": \"ipc\",\n\"path\": \"/proc/1/ns/ipc\"?" "$TEMP_DIR/dest/config.json"

}

function teardown() {
    cleanup_tempdir
}

@test "manual invocation" {
    crio-lxc --debug --log-level trace --log-file "$TEMP_DIR/log" create --bundle "$TEMP_DIR/dest" --pid-file "$TEMP_DIR/pid" alpine
    crio-lxc --debug --log-level trace --log-file "$TEMP_DIR/log" start alpine
    pid1ipcnsinode=$(stat -L -c%i /proc/1/ns/ipc)
    mypid=$(<"$TEMP_DIR/pid")
    mypidipcnsinode=$(stat -L -c%i "/proc/$mypid/ns/ipc")
    [ $pid1ipcnsinode = $mypidipcnsinode ]
    crio-lxc --debug --log-level trace --log-file "$TEMP_DIR/log" kill alpine
    crio-lxc --debug --log-level trace --log-file "$TEMP_DIR/log" delete alpine
}
