COMMIT_HASH=$(shell git describe --always --tags --long)
COMMIT=$(if $(shell git status --porcelain --untracked-files=no),$(COMMIT_HASH)-dirty,$(COMMIT_HASH))
BINS := lxcri-start lxcri-init lxcri-container-hook lxcri
# Installation prefix for BINS
PREFIX ?= /usr/local
# Note: The default pkg-config directory is search after PKG_CONFIG_PATH
PKG_CONFIG_PATH ?= /usr/local/lib/pkgconfig
export PKG_CONFIG_PATH
LIBLXC_LDFLAGS = $(shell pkg-config --libs --cflags lxc)
LDFLAGS=-X main.version=$(COMMIT)
CC ?= cc
MUSL_CC ?= musl-gcc

all: fmt test $(BINS)

install: $(BINS)
	cp -v $(BINS) $(PREFIX)/bin

.PHONY: clean
clean:
	-rm -f $(BINS)

.PHONY: test
test:
	go test -v ./...

lint:
	golangci-lint run -c ./lint.yaml ./...

lxcri: go.mod **/*.go
	go build -a -ldflags '$(LDFLAGS)' -o $@ ./cmd

lxcri-start: cmd/start/lxcri-start.c
	$(CC) -Werror -Wpedantic -o $@ $? $(LIBLXC_LDFLAGS)

lxcri-init: cmd/init/lxcri-init.c
	$(MUSL_CC) -Werror -Wpedantic -static -o $@ $?
	# this is paranoia - but ensure it is statically compiled
	! ldd $@  2>/dev/null

lxcri-container-hook: cmd/container-hook/hook.c
	$(MUSL_CC) -Werror -Wpedantic -static -o $@ $?

fmt:
	go fmt ./...
