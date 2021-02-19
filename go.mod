module github.com/lxc/crio-lxc

require (
	github.com/creack/pty v1.1.11
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/pretty v0.2.1 // indirect
	github.com/opencontainers/runtime-spec v1.0.3-0.20200929063507-e6143ca7d51d
	github.com/rs/zerolog v1.20.0
	github.com/stretchr/testify v1.4.0
	github.com/urfave/cli/v2 v2.3.0
	golang.org/x/sys v0.0.0-20210119212857-b64e53b001e4
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	gopkg.in/lxc/go-lxc.v2 v2.0.0-20210205143421-c4b883be4881
	gopkg.in/yaml.v2 v2.4.0 // indirect
)

replace golang.org/x/crypto => golang.org/x/crypto v0.0.0-20201221181555-eec23a3978ad

replace golang.org/x/text => golang.org/x/text v0.3.3

go 1.13
