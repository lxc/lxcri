all: crio-lxc-init

crio-lxc-init: crio-lxc-init.c
	/usr/local/musl/bin/musl-gcc -Wpedantic -Wall  -static -g  -o $@ $? 
	#musl-gcc -g3 -Wall -static $? -o $@
	# ensure that crio-lxc-init is statically compiled
	! ldd $@  2>/dev/null
