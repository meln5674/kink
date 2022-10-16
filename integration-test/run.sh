#!/bin/bash -xe

which go || (echo "go not on PATH" ; exit 1)
which kind || (echo "kind not on PATH" ; exit 1)
which kubectl || (echo "kubectl not on PATH" ; exit 1)
which helm || (echo "helm not on PATH" ; exit 1)

mkdir -p bin

# go test -covermode=atomic -coverpkg="./..." -o bin/kink.cover ./... # go 1.19 maybe?

go build -o bin/kink.cover main.go

TEST_TIMESTAMP=$(date +%s)
KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME=kink}

IMAGE_REPO=${IMAGE_REPO:-local.host/meln5674/kink}
IMAGE_TAG=${IMAGE_TAG:-${TEST_TIMESTAMP}}

BUILT_IMAGE=${IMAGE_REPO}:${IMAGE_TAG}

export DOCKER_BUILDKIT=1
export KUBECONFIG=./integration-test/kind.kubeconfig


docker build -t "${BUILT_IMAGE}" .
CLUSTER_EXISTS="$(
    if kind get clusters | grep -qw "${KIND_CLUSTER_NAME}" ; then
        echo 1
    fi
)"
if [ -z "${CLUSTER_EXISTS}" ] || ([ -n "${CLUSTER_EXISTS}" ] && [ -z "${KINK_IT_NO_CLEANUP}" ]); then
    kind create cluster \
        --name="${KIND_CLUSTER_NAME}" \
        --kubeconfig="${KUBECONFIG}"
fi
if [ -z "${KINK_IT_NO_CLEANUP}" ]; then
    TRAP_CMD="kind delete cluster --name='${KIND_CLUSTER_NAME}'"
    trap "${TRAP_CMD}" EXIT
fi

kind load docker-image "${BUILT_IMAGE}" --name="${KIND_CLUSTER_NAME}"

bin/kink.cover create cluster \
    --chart ./helm/kink \
    --set image.repository="${IMAGE_REPO}" \
    --set image.tag="${IMAGE_TAG}" \
    --set image.pullPolicy=Never \
    --set controlplane.securityContext.privileged=true \
    --set worker.securityContext.privileged=true \
    --set controlplane.replicaCount=1 \
    --set worker.replicaCount=1 \
    --set controlplane.persistence.enabled=true \
    --set worker.persistence.enabled=true 


bin/kink.cover get cluster | tee /dev/stderr | grep "kink-kink"

if [ -z "${KINK_IT_NO_CLEANUP}" ]; then
    bin/kink.cover delete cluster
fi

WORDPRESS_CHART_VERSION=15.2.5

WORDPRESS_IMAGE=docker.io/bitnami/wordpress:6.0.2-debian-11-r9
MARIADB_IMAGE=docker.io/bitnami/mariadb:10.6.10-debian-11-r0
MEMCACHED_IMAGE=docker.io/bitnami/memcached:1.6.17-debian-11-r6

docker pull "${WORDPRESS_IMAGE}" 
#bin/kink.cover load docker-image --image "${WORDPRESS_IMAGE}" --parallel-loads=-1

docker pull "${MARIADB_IMAGE}"
docker save "${MARIADB_IMAGE}" > ./integration-test/mariadb.tar
bin/kink.cover load docker-archive --archive ./integration-test/mariadb.tar

buildah build-using-dockerfile \
    --file - \
    --tag "${MEMCACHED_IMAGE}" \
    <<< "FROM ${MEMCACHED_IMAGE}"
buildah push "${MEMCACHED_IMAGE}" oci-archive:./integration-test/memcached-image.tar
bin/kink.cover load oci-archive --archive ./integration-test/memcached-image.tar

helm repo add bitnami https://charts.bitnami.com/bitnami

bin/kink.cover sh -- '
    while ! kubectl version ; do
        sleep 10;
    done
    helm upgrade --install wordpress bitnami/wordpress \
        --set persistence.enabled=true \
        --set mariadb.enabled=true \
        --set memcached.enabled=true \
        --set service.type=ClusterIP \
        --set ingress.enabled=true \
        --debug
'
if [ -z "${KINK_IT_NO_CLEANUP}" ]; then
    TRAP_CMD="bin/kink.cover sh helm delete wordpress ; ${TRAP_CMD}"
    trap "${TRAP_CMD}" EXIT
fi


