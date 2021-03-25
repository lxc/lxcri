COMMIT_HASH = $(shell git describe --always --tags --long)
COMMIT = $(if $(shell git status --porcelain --untracked-files=no),$(COMMIT_HASH)-dirty,$(COMMIT_HASH))
BINS := lxcri-start lxcri-init lxcri-hook lxcri
# Installation prefix for BINS
PREFIX ?= /usr/local
# Note: The default pkg-config directory is search after PKG_CONFIG_PATH
PKG_CONFIG_PATH ?= /usr/local/lib/pkgconfig
export PKG_CONFIG_PATH
LIBLXC_LDFLAGS = $(shell pkg-config --libs --cflags lxc)
LDFLAGS=-X main.version=$(COMMIT)
CC ?= cc
MUSL_CC ?= musl-gcc
SHELL_SCRIPTS = $(shell find . -name \*.sh)
GO_SRC = $(shell find . -name \*.go)

all: fmt test build

update-tools:
	GO111MODULE=off go get -u mvdan.cc/sh/v3/cmd/shfmt
	GO111MODULE=off go get -u golang.org/x/lint/golint

fmt:
	go fmt ./...
	shfmt -w $(SHELL_SCRIPTS)
	golint ./...
	go mod tidy

.PHONY: test
test:
	go test -v ./...

build: $(BINS)

lxcri: go.mod $(GO_SRC)
	go build -a -ldflags '$(LDFLAGS)' -o $@ ./cmd/lxcri

lxcri-start: cmd/lxcri-start/lxcri-start.c
	$(CC) -Werror -Wpedantic -o $@ $? $(LIBLXC_LDFLAGS)

lxcri-init: cmd/lxcri-init/lxcri-init.c
	$(MUSL_CC) -Werror -Wpedantic -static -o $@ $?
	# this is paranoia - but ensure it is statically compiled
	! ldd $@  2>/dev/null

lxcri-hook: cmd/lxcri-hook/lxcri-hook.c
	$(MUSL_CC) -Werror -Wpedantic -static -o $@ $?

install: build
	cp -v $(BINS) $(PREFIX)/bin

.PHONY: clean
clean:
	-rm -f $(BINS)

