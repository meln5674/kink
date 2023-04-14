#!/bin/bash -xue

kind delete cluster --name=kink-it || true
volumes=$(readlink integration-test/volumes)
if [ -z "${volumes}" ]; then
    volumes=${PWD}/integration-test/volumes
fi
docker run --rm -it -v ${volumes}://src/ alpine:3 sh -xec 'rm -rf /src/*'
