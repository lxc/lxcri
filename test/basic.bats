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
    podid=$(crictl pods | grep nginx-sandbox | awk '{ print $1 }')
    crictl create $podid test/basic-container-config.json test/basic-pod-config.json
    [ "$(crictl ps -a)" | grep busybox ]
}
