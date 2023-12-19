#!/bin/bash -xeu

OWNER="$(id -u):$(id -g)"

docker run --rm -i -v "${PWD}/bin/cov://mnt/" -w //mnt alpine:3 sh -xe <<EOF
    chown -R '${OWNER}' ./
EOF

