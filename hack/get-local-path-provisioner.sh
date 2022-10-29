#!/bin/bash -xe

rm -rf charts/local-path-provisioner

git clone \
    https://github.com/rancher/local-path-provisioner.git \
    charts/local-path-provisioner

helm package \
    charts/local-path-provisioner/deploy/chart/local-path-provisioner \
    --destination charts

CHART_VERSION=$(helm show chart ./charts/local-path-provisioner/deploy/chart/local-path-provisioner/ | grep -E '^version: ' | sed -E 's/^version: //g')

KINK_REF="$(git rev-parse HEAD)" \
CHART_PATH="charts/local-path-provisioner-${CHART_VERSION}.tgz" \
hack/mk-local-path-provisioner.sh > charts/local-path-provisioner.yaml
