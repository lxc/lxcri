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

