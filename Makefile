GO_SRC=$(shell find . -name \*.go)
COMMIT_HASH=$(shell git describe --always --tags --long)
COMMIT=$(if $(shell git status --porcelain --untracked-files=no),$(COMMIT_HASH)-dirty,$(COMMIT_HASH))
TEST?=$(patsubst test/%.bats,%,$(wildcard test/*.bats))
PACKAGES_DIR?=~/packages
BINS := crio-lxc crio-lxc-start crio-lxc-init crio-lxc-container-hook
PREFIX ?= /usr/local
PKG_CONFIG_PATH ?= $(PREFIX)/lib/pkgconfig
export PKG_CONFIG_PATH
LDFLAGS=-X main.version=$(COMMIT)
CC ?= cc
MUSL_CC ?= musl-gcc

all: fmt test $(BINS)

install: all
	cp -v $(BINS) $(PREFIX)/bin

.PHONY: test
test:
	go test -v ./...

lint:
	golangci-lint run -c ./lint.yaml ./...

crio-lxc: $(GO_SRC) Makefile go.mod
	go build -a -ldflags '$(LDFLAGS)' -o $@ ./cmd

crio-lxc-start: cmd/start/crio-lxc-start.c
	$(CC) -Werror -Wpedantic $(shell PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) pkg-config --libs --cflags lxc) -o $@ $?

crio-lxc-init: cmd/init/crio-lxc-init.c
	$(MUSL_CC) -Werror -Wpedantic -static -g -o $@ $?
	#musl-gcc -g3 -Werror -static $? -o $@
	# ensure that crio-lxc-init is statically compiled
	! ldd $@  2>/dev/null

crio-lxc-container-hook: cmd/container-hook/hook.c
	musl-gcc -DDEBUG -Werror -Wpedantic $? -o $@

# make test TEST=basic will run only the basic test.
.PHONY: check
check: crio-lxc
	go fmt ./... && ([ -z $(TRAVIS) ] || git diff --quiet)
	go test ./...
	PACKAGES_DIR=$(PACKAGES_DIR) sudo -E "PATH=$$PATH" bats -t $(patsubst %,test/%.bats,$(TEST))

.PHONY: vendorup
vendorup:
	go get -u

.PHONY: clean
clean:
	-rm -f $(BINS)

fmt:
	go fmt ./...
