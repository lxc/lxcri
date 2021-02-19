# Packaging

    docker build .

    docker build --no-cache -t helloap:0.1 -f ./Dockerfile  .

Run with buildah as fast as docker

    buildah bud ubuntu.20.04.Dockerfile

    buildah bud --no-cache --pull-never Dockerfile

## TODO

* Build Images with Github ?
* Automate script formatting with [shfmt](https://github.com/mvdan/sh)

    ~/go/bin/shfmt -w ubuntu-install-lxc.sh ubuntu-install-lxc.sh


## Buildah Oneliners

#### Remove all working containers

   buildah containers -q | xargs buildah delete

#### Remove all images without a name

    buildah images | grep '<none>' | tr -s ' ' | cut -d ' ' -f 3 | xargs buildah rmi

#### Select images by name with jq

    buildah images --json |  jq '[ .[] | select( .names[] | contains("localhost")) ]'


## Resources

* [Best practices for writing Dockerfiles](https://docs.docker.com/develop/develop-images/dockerfile_best-practices/)
* [Reducing the size of the Debian Installation Footprint](https://wiki.debian.org/ReduceDebian)


## Container Changlog

Provide some information about what has changed in an container upgrade.

* Changelog for package upgrades
* List of Modified files (mode and size, maybe rsync like output ?)

## apt Package Changelog for debian/ubuntu

Create a list of installed packages and versions.

https://man7.org/linux/man-pages/man1/dpkg-query.1.html

dpkg-query -W -f='{Pkg:"${binary:Package}", Version:"${Version}",Category:"${Section}"}\n'

## List changed files

### Packages installed from source ?

* Are handled by the image layer
* Before squashing the image changes in the image layer should be listed

### Track file changes with overlay

* mount overlay on rootfs, make install, unmount, tar overlay and create package from it.
* similar to archlinux `makepkg` ...
