module github.com/lxc/crio-lxc

require (
	github.com/creack/pty v1.1.11
	github.com/opencontainers/runtime-spec v1.0.2
	github.com/pkg/errors v0.9.1
	github.com/rs/zerolog v1.20.0
	github.com/stretchr/testify v1.3.0
	github.com/urfave/cli/v2 v2.3.0
	golang.org/x/sys v0.0.0-20201029080932-201ba4db2418
	gopkg.in/lxc/go-lxc.v2 v2.0.0
)

replace gopkg.in/lxc/go-lxc.v2 v2.0.0 => github.com/Drachenfels-GmbH/go-lxc v0.0.0-20201106192530-079aead12fef

replace github.com/vbatts/go-mtree v0.4.4 => github.com/vbatts/go-mtree v0.4.5-0.20190122034725-8b6de6073c1a

replace github.com/openSUSE/umoci v0.4.4 => github.com/tych0/umoci v0.1.1-0.20190402232331-556620754fb1

go 1.13
