COMMIT_HASH = $(shell git describe --always --tags --long)
COMMIT = $(if $(shell git status --porcelain --untracked-files=no),$(COMMIT_HASH)-dirty,$(COMMIT_HASH))
BINS := lxcri
LIBEXEC_BINS := lxcri-start lxcri-init lxcri-hook lxcri-hook-builtin
# Installation prefix for BINS
PREFIX ?= /usr/local
export PREFIX
LIBEXEC_DIR = $(PREFIX)/libexec/lxcri
export LIBEXEC_DIR
PKG_CONFIG_PATH ?= $(PREFIX)/lib/pkgconfig
# Note: The default pkg-config directory is search after PKG_CONFIG_PATH
# Note: (Exported) environment variables are NOT visible in the environment of the $(shell ...) function.
export PKG_CONFIG_PATH
LDFLAGS=-X main.version=$(COMMIT) -X main.libexecDir=$(LIBEXEC_DIR)
CC ?= cc
SHELL_SCRIPTS = $(shell find . -name \*.sh)
GO_SRC = $(shell find . -name \*.go | grep -v _test.go)

all: fmt test

update-tools:
	GO111MODULE=off go get -u mvdan.cc/sh/v3/cmd/shfmt
	GO111MODULE=off go get -u golang.org/x/lint/golint

fmt:
	go fmt ./...
	shfmt -w $(SHELL_SCRIPTS)
	golint ./...
	go mod tidy

.PHONY: test
test: build
	go build ./cmd/lxcri-test
	go test --count 1 -v ./...

build: $(BINS) $(LIBEXEC_BINS)

lxcri: go.mod $(GO_SRC) Makefile
	go build -ldflags '$(LDFLAGS)' -o $@ ./cmd/lxcri

lxcri-start: cmd/lxcri-start/lxcri-start.c
	$(CC) -Werror -Wpedantic -o $@ $? $$(pkg-config --libs --cflags lxc)

lxcri-init: go.mod $(GO_SRC) Makefile
	CGO_ENABLED=0 go build -o $@ ./cmd/lxcri-init
	# this is paranoia - but ensure it is statically compiled
	! ldd $@  2>/dev/null

lxcri-hook: go.mod $(GO_SRC) Makefile
	go build -o $@ ./cmd/$@

lxcri-hook-builtin: go.mod $(GO_SRC) Makefile
	go build -o $@ ./cmd/$@

install: build
	mkdir -p $(PREFIX)/bin
	cp -v $(BINS) $(PREFIX)/bin
	mkdir -p $(LIBEXEC_DIR)
	cp -v $(LIBEXEC_BINS) $(LIBEXEC_DIR)

.PHONY: clean
clean:
	-rm -f $(BINS) $(LIBEXEC_BINS)

