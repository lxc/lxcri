load helpers

function setup() {
    setup_crio
}

function teardown() {
    cleanup_crio
}

@test "basic cri-o workings" {
    crictl runp test/basic-pod-config.json
    crictl pull busybox
    crictl images
    crictl create clowncore test/basic-container-config.json test/basic-pod-config.json
    [ "$(crictl ps)" | grep clowncore ]
}
