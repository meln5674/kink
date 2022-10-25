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

docker build -t "${BUILT_IMAGE}" .
CLUSTER_EXISTS="$(
    if kind get clusters | grep -qw "${KIND_CLUSTER_NAME}" ; then
        echo 1
    fi
)"
if [ -z "${CLUSTER_EXISTS}" ] || ([ -n "${CLUSTER_EXISTS}" ] && [ -z "${KINK_IT_NO_CLEANUP}" ] && [ -z "${KINK_IT_CLEANUP}"]); then
    kind create cluster \
        --name="${KIND_CLUSTER_NAME}" \
        --kubeconfig="${KUBECONFIG}"
fi
if [ -z "${KINK_IT_NO_CLEANUP}" ]; then
    TRAP_CMD="kind delete cluster --name='${KIND_CLUSTER_NAME}'"
    trap "${TRAP_CMD}" EXIT
fi

if [ -n "${KINK_IT_CLEANUP}" ]; then
    kink delete cluster
    kind delete cluster --name="${KIND_CLUSTER_NAME}"
    exit 0
fi

kind load docker-image "${BUILT_IMAGE}" --name="${KIND_CLUSTER_NAME}"

KINK_CLUSTER_NAME=it
KINK_CONFIG_FILE=integration-test/kink.config.yaml

KINK_COMMAND=( bin/kink.cover --config "${KINK_CONFIG_FILE}" --name "${KINK_CLUSTER_NAME}" -v11 )


"${KINK_COMMAND[@]}" create cluster \
    --set image.repository="${IMAGE_REPO}" \
    --set image.tag="${IMAGE_TAG}"
    

"${KINK_COMMAND[@]}" get cluster | tee /dev/stderr | grep "kink-${KINK_CLUSTER_NAME}"


if [ -z "${KINK_IT_NO_CLEANUP}" ]; then
    "${KINK_COMMAND[@]}" delete cluster
fi

WORDPRESS_CHART_VERSION=15.2.5

WORDPRESS_IMAGE=docker.io/bitnami/wordpress:6.0.2-debian-11-r9
MARIADB_IMAGE=docker.io/bitnami/mariadb:10.6.10-debian-11-r0
MEMCACHED_IMAGE=docker.io/bitnami/memcached:1.6.17-debian-11-r6

docker pull "${WORDPRESS_IMAGE}" 
"${KINK_COMMAND[@]}" load docker-image \
    --config "${KINK_CONFIG_FILE}" \
    --name "${KINK_CLUSTER_NAME}" \
    --image "${WORDPRESS_IMAGE}" \
    --parallel-loads=1

docker pull "${MARIADB_IMAGE}"
docker save "${MARIADB_IMAGE}" > ./integration-test/mariadb.tar
"${KINK_COMMAND[@]}" load docker-archive --archive ./integration-test/mariadb.tar

buildah build-using-dockerfile \
    --file - \
    --tag "${MEMCACHED_IMAGE}" \
    <<< "FROM ${MEMCACHED_IMAGE}"
buildah push "${MEMCACHED_IMAGE}" oci-archive:./integration-test/memcached-image.tar
"${KINK_COMMAND[@]}" load oci-archive --archive ./integration-test/memcached-image.tar

helm repo add bitnami https://charts.bitnami.com/bitnami

"${KINK_COMMAND[@]}" sh -- '
    while ! kubectl version ; do
        sleep 10;
    done
    kubectl cluster-info
    kubectl get nodes

    helm upgrade --install wordpress bitnami/wordpress \
        --wait \
        --set persistence.enabled=true \
        --set mariadb.enabled=true \
        --set memcached.enabled=true \
        --set service.type=ClusterIP \
        --set ingress.enabled=true \
        --debug


    kubectl get all -A
    kubectl port-forward svc/wordpress 8080:80 &
    sleep 5
    curl -v http://localhost:8080
    kill %1
'

KINK_KUBECONFIG=integration-test/kink.kubeconfig

"${KINK_COMMAND[@]}" export kubeconfig --out-kubeconfig="${KINK_KUBECONFIG}"

cat "${KINK_KUBECONFIG}"

if [ -z "${KINK_IT_NO_CLEANUP}" ]; then
    TRAP_CMD="bin/kink.cover exec -- helm delete wordpress ; ${TRAP_CMD}"
    trap "${TRAP_CMD}" EXIT
fi


