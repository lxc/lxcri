module github.com/lxc/crio-lxc

require (
	github.com/anuvu/stacker v0.4.0
	github.com/apex/log v1.1.0
	github.com/gorilla/websocket v1.4.0 // indirect
	github.com/juju/loggo v0.0.0-20190212223446-d976af380377 // indirect
	github.com/lxc/lxd v0.0.0-20190404234020-f51c28a37443
	github.com/openSUSE/umoci v0.4.4
	github.com/opencontainers/runtime-spec v1.0.1
	github.com/opencontainers/selinux v1.2.1 // indirect
	github.com/pkg/errors v0.8.1
	github.com/rogpeppe/godef v1.1.1 // indirect
	github.com/sirupsen/logrus v1.4.1 // indirect
	github.com/urfave/cli v1.20.0
	golang.org/x/crypto v0.0.0-20190404164418-38d8ce5564a5 // indirect
	golang.org/x/net v0.0.0-20190404232315-eb5bcb51f2a3 // indirect
	golang.org/x/sys v0.0.0-20190405154228-4b34438f7a67
	golang.org/x/tools v0.0.0-20190405180640-052fc3cfdbc2 // indirect
	gopkg.in/check.v1 v1.0.0-20180628173108-788fd7840127 // indirect
	gopkg.in/lxc/go-lxc.v2 v2.0.0-20190324192716-2f350e4a2980
	gopkg.in/yaml.v2 v2.2.2
	k8s.io/client-go v11.0.0+incompatible // indirect
)

replace github.com/vbatts/go-mtree v0.4.4 => github.com/vbatts/go-mtree v0.4.5-0.20190122034725-8b6de6073c1a

replace github.com/openSUSE/umoci v0.4.4 => github.com/tych0/umoci v0.1.1-0.20190402232331-556620754fb1
