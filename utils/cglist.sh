#!/bin/sh

cd /sys/fs/cgroup/kubepods.slice

for cg in $(find . -name cgroup.controllers); do
	echo "$(dirname $cg) [$(cat $cg)]"
done
