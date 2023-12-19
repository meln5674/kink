#!/bin/bash -xeu

docker run --rm -i -v "${PWD}/integration-test/volumes://mnt/" -w //mnt alpine:3 sh -xe <<'EOF'
rm -rf \
    var/lib/kubelet/* \
    var/lib/containerd/*/* \
    var/log/* \
    var/local-path-provisioner/* \
    var/shared-local-path-provisioner/*
EOF

