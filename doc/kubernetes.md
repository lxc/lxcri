## kubernetes

The following skript downloads kubernetes [v1.20.2](https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG/CHANGELOG-1.20.md#v1202) and installs it to `/usr/local/bin`.</br>
You have to create the `kubelet.service` and `10-kubeadm.conf` before running the script.

```sh
#!/bin/sh
# about: installs kubeadm,kubectl and kubelet to /usr/local/bin
# installs systemd service to /etc/systemd/system 


# Upgrade process:
# * change RELEASE and CHECKSUM
# * remove downloaded archive file
# * run this script again

ARCH="linux-amd64"
RELEASE="1.20.2"
ARCHIVE=kubernetes-server-$ARCH.tar.gz
CHECKSUM="65abf178782e43bc21e8455ffbfdadf6064dbeae3ff704ccf9e13e8acee18235c280b06778e5de4bd702f5507e1870fe38c561366d125ef4f821ed7aa46e9f45"
DESTDIR="/usr/local/bin"

[ -e "$ARCHIVE" ] || wget https://dl.k8s.io/v$RELEASE/$ARCHIVE

echo "$CHECKSUM $ARCHIVE" | sha512sum -c || exit 1

tar -x -z -f $ARCHIVE -C $DESTDIR --strip-components=3 kubernetes/server/bin/kubectl kubernetes/server/bin/kubeadm kubernetes/server/bin/kubelet
install -v kubelet.service /etc/systemd/system/
install -v -D 10-kubeadm.conf /etc/systemd/system/kubelet.service.d/10-kubeadm.conf
systemctl daemon-reload
```

### systemd service

**kubelet.service**
```
[Unit]
Description=kubelet: The Kubernetes Node Agent
Documentation=http://kubernetes.io/docs/

[Service]
ExecStart=/usr/local/bin/kubelet
Restart=always
StartLimitInterval=0
RestartSec=10

[Install]
WantedBy=multi-user.target
```

**10-kubeadm.conf**
```
# Note: This dropin only works with kubeadm and kubelet v1.11+
[Service]
Environment="KUBELET_KUBECONFIG_ARGS=--bootstrap-kubeconfig=/etc/kubernetes/bootstrap-kubelet.conf --kubeconfig=/etc/kubernetes/kubelet.conf"
Environment="KUBELET_CONFIG_ARGS=--config=/var/lib/kubelet/config.yaml"
# This is a file that "kubeadm init" and "kubeadm join" generate at runtime, populating the KUBELET_KUBEADM_ARGS variable dynamically
EnvironmentFile=-/var/lib/kubelet/kubeadm-flags.env
# This is a file that the user can use for overrides of the kubelet args as a last resort. Preferably, the user should use
# the .NodeRegistration.KubeletExtraArgs object in the configuration files instead. KUBELET_EXTRA_ARGS should be sourced from this file.
EnvironmentFile=-/etc/default/kubelet
ExecStart=
ExecStart=/usr/local/bin/kubelet $KUBELET_KUBECONFIG_ARGS $KUBELET_CONFIG_ARGS $KUBELET_KUBEADM_ARGS $KUBELET_EXTRA_ARGS
```

### kubeadm init

This initializes the kubernetes control-plane.

* Replace `HOSTIP` and `HOSTNAME` variables in  `cluster-init.yaml` and initialize the cluster:

```
kubeadm init --config cluster-init.yaml -v 5
# for single node cluster remove taint
taint remove kubectl taint nodes --all node-role.kubernetes.io/master-
```
 
 * Install a networking plugin (I'm using [calico](https://www.projectcalico.org))

**cluster-init.yaml**
```yaml
apiVersion: kubeadm.k8s.io/v1beta2
kind: InitConfiguration
localAPIEndpoint:
  advertiseAddress: {HOSTIP}
  bindPort: 6443
nodeRegistration:
  name: {HOSTNAME}
  criSocket: unix://var/run/crio/crio.sock
  taints:
  - effect: NoSchedule
    key: node-role.kubernetes.io/master
#  kubeletExtraArgs:
#   v: "5"
---
apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
cgroupDriver: systemd
---
kind: ClusterConfiguration
kubernetesVersion: v1.20.2
apiVersion: kubeadm.k8s.io/v1beta2
apiServer:
  timeoutForControlPlane: 4m0s
certificatesDir: /etc/kubernetes/pki
clusterName: kubernetes
controllerManager: {}
dns:
  type: CoreDNS
etcd:
  local:
    dataDir: /var/lib/etcd
imageRepository: k8s.gcr.io
networking:
  dnsDomain: cluster.local
  serviceSubnet: 10.96.0.0/12
  podSubnet: 10.66.0.0/16
scheduler: {}
controlPlaneEndpoint: "${HOSTIP}:6443"
```

#### preflight issues

There are some `preflight` checks that might fail once you start kubeadm.

##### install cri-tools

```
[ERROR FileExisting-crictl]: crictl not found in system path
```

Please install the `cri-tools` from https://github.com/kubernetes-sigs/cri-tools/releases to your `PATH` e.g [cri-tools v1.20.0](https://github.com/kubernetes-sigs/cri-tools/releases/download/v1.20.0/crictl-v1.20.0-linux-amd64.tar.gz)


##### load br-netfilter

```
[ERROR FileContent--proc-sys-net-bridge-bridge-nf-call-iptables]: /proc/sys/net/bridge/bridge-nf-call-iptables does not exist
```

You must load the `br-netfilter` kernel module. To do that automatically on startup add it to `/etc/modules-load.d` e.g :

```
echo 'br-netfilter' > /etc/modules-load.d/kubelet.conf
```

##### enable IP forwarding
```
[ERROR FileContent--proc-sys-net-ipv4-ip_forward]: /proc/sys/net/ipv4/ip_forward contents are not set to 1
````

IP forwarding must be enabled. E.g

```
echo 'net.ipv4.ip_forward=1' > /etc/sysctl.d/99-kubelet.conf
sysctl --system
```
