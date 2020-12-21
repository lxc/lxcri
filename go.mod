module github.com/lxc/crio-lxc

require (
	github.com/creack/pty v1.1.11
	github.com/opencontainers/runtime-spec v1.0.2
	github.com/pkg/errors v0.9.1
	github.com/rs/zerolog v1.20.0
	github.com/securego/gosec/v2 v2.5.0 // indirect
	github.com/stretchr/testify v1.4.0
	github.com/urfave/cli v1.22.5 // indirect
	github.com/urfave/cli/v2 v2.2.0
	golang.org/x/crypto v0.0.0-20201016220609-9e8e0b390897 // indirect
	golang.org/x/lint v0.0.0-20200302205851-738671d3881b // indirect
	golang.org/x/net v0.0.0-20201110031124-69a78807bb2b // indirect
	golang.org/x/sys v0.0.0-20201110211018-35f3e6cf4a65
	golang.org/x/text v0.3.4 // indirect
	golang.org/x/tools v0.0.0-20201110201400-7099162a900a // indirect
	gopkg.in/lxc/go-lxc.v2 v2.0.0-20200826211823-2dd0dc9c018b
)

replace github.com/vbatts/go-mtree v0.4.4 => github.com/vbatts/go-mtree v0.4.5-0.20190122034725-8b6de6073c1a

replace github.com/openSUSE/umoci v0.4.4 => github.com/tych0/umoci v0.1.1-0.20190402232331-556620754fb1

go 1.13
