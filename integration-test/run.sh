#!/bin/bash -xe

which go || (echo "go not on PATH" ; exit 1)
which kind || (echo "kind not on PATH" ; exit 1)
which kubectl || (echo "kubectl not on PATH" ; exit 1)
which helm || (echo "helm not on PATH" ; exit 1)
which docker || (echo "docker not on PATH" ; exit 1)

mkdir -p bin

# go test -covermode=atomic -coverpkg="./..." -o bin/kink.cover ./... # go 1.19 maybe?

TEST_TIMESTAMP=$(date +%s)
go build -o bin/kink.cover main.go

IMAGE_REPO=${IMAGE_REPO:-local.host/meln5674/kink}
if [ -z "${KINK_IT_CLEANUP}" ] && [ -z "${KINK_IT_NO_DOCKER_BUILD}" ] ; then
    IMAGE_TAG=${IMAGE_TAG:-${TEST_TIMESTAMP}}
    BUILT_IMAGE=${IMAGE_REPO}:${IMAGE_TAG}
    export DOCKER_BUILDKIT=1
    docker build -t "${BUILT_IMAGE}" .
    echo "${BUILT_IMAGE}" > integration-test/last-image
else
    BUILT_IMAGE=$(cat integration-test/last-image)
    IMAGE_REPO=$(awk -F ':' '{ print $1 }' <<< "${BUILT_IMAGE}")
    IMAGE_TAG=$(awk -F ':' '{ print $2 }' <<< "${BUILT_IMAGE}")
fi

KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME=kink}
CLUSTER_EXISTS="$(
    if kind get clusters | grep -qw "${KIND_CLUSTER_NAME}" ; then
        echo 1
    fi
)"

KIND_KUBECONFIG=./integration-test/kind.kubeconfig
if [ -z "${CLUSTER_EXISTS}" ] || ([ -n "${CLUSTER_EXISTS}" ] && [ -z "${KINK_IT_NO_CLEANUP}" ] && [ -z "${KINK_IT_CLEANUP}"]); then
    kind create cluster \
        --name="${KIND_CLUSTER_NAME}" \
        --kubeconfig="${KIND_KUBECONFIG}"
fi

if [ -z "${KINK_IT_NO_CLEANUP}" ]; then
    TRAP_CMD="kind delete cluster --name='${KIND_CLUSTER_NAME}'"
    trap "${TRAP_CMD}" EXIT
fi

if [ -z "${KINK_IT_CLEANUP}" ] ; then
    kind load docker-image "${BUILT_IMAGE}" --name="${KIND_CLUSTER_NAME}"
fi

if [ -n "${KINK_IT_CLEANUP}" ]; then

    for test_case in k3s k3s-ha rke2; do
        KINK_CLUSTER_NAME=it-${test_case}
        KINK_COMMAND=( bin/kink.cover --config "${KINK_CONFIG_FILE}" --name "${KINK_CLUSTER_NAME}" -v11 )
        "${KINK_COMMAND[@]}" delete cluster --name="${KINK_CLUSTER_NAME}" || true
    done
    kind delete cluster --name="${KIND_CLUSTER_NAME}"
    exit 0
fi


export KUBECONFIG="${KIND_KUBECONFIG}"

kubectl get pods -w &
GET_PODS_PID=$!
TRAP_CMD="kill ${GET_PODS_PID} ; ${TRAP_CMD}"
trap "${TRAP_CMD}" EXIT

for test_case in ${TEST_CASES:-k3s k3s-ha rke2}; do

    export KUBECONFIG="${KIND_KUBECONFIG}"

    KINK_CLUSTER_NAME=it-${test_case}
    KINK_CONFIG_FILE=integration-test/kink.${test_case}.config.yaml

    KINK_COMMAND=( bin/kink.cover --config "${KINK_CONFIG_FILE}" --name "${KINK_CLUSTER_NAME}" -v11 )

    KINK_KUBECONFIG=integration-test/kink-${test_case}.kubeconfig

    if ! ("${KINK_COMMAND[@]}" get cluster | tee /dev/stderr | grep "kink-${KINK_CLUSTER_NAME}") || [ -z "${KINK_IT_NO_KINK_CREATE}" ]; then
        "${KINK_COMMAND[@]}" create cluster \
            --set image.repository="${IMAGE_REPO}" \
            --set image.tag="${IMAGE_TAG}" \
            --out-kubeconfig="${KINK_KUBECONFIG}"
    fi

    if [ -z "${KINK_IT_NO_CLEANUP}" ]; then
        TRAP_CMD="${KINK_COMMAND[@]} delete cluster; ${TRAP_CMD}"
        trap "${TRAP_CMD}" EXIT
    fi

    "${KINK_COMMAND[@]}" sh -- '
        set -xe
        while ! kubectl version ; do
            sleep 10;
        done
        kubectl cluster-info
        kubectl get nodes
    '

    WORDPRESS_CHART_VERSION=15.2.5

    WORDPRESS_IMAGE=docker.io/bitnami/wordpress:6.0.3-debian-11-r3
    MARIADB_IMAGE=docker.io/bitnami/mariadb:10.6.10-debian-11-r6
    MEMCACHED_IMAGE=docker.io/bitnami/memcached:1.6.17-debian-11-r15

    if [ -z "${KINK_IT_NO_LOAD}" ]; then 
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
        buildah push "${MEMCACHED_IMAGE}" "oci-archive:./integration-test/memcached-image.tar:${MEMCACHED_IMAGE}"
        "${KINK_COMMAND[@]}" load oci-archive --archive ./integration-test/memcached-image.tar
    fi

    helm repo add bitnami https://charts.bitnami.com/bitnami

    "${KINK_COMMAND[@]}" sh --exported-kubeconfig="${KINK_KUBECONFIG}" -- '
        set -xe
        while ! kubectl version ; do
            sleep 10;
        done
        kubectl cluster-info
        kubectl get nodes

        helm upgrade --install wordpress bitnami/wordpress \
            --version 15.2.7 \
            --wait \
            --set persistence.enabled=true \
            --set mariadb.enabled=true \
            --set memcached.enabled=true \
            --set service.type=ClusterIP \
            --set ingress.enabled=true \
            --set image.pullPolicy=Never \
            --set mariadb.image.pullPolicy=Never \
            --set memcached.image.pullPolicy=Never \
            --debug


        kubectl get all -A
        kubectl port-forward svc/wordpress 8080:80 &
        PORT_FORWARD_PID=$!
        trap "kill ${PORT_FORWARD_PID}" EXIT
        sleep 5
        curl -v http://localhost:8080
    '

    "${KINK_COMMAND[@]}" export kubeconfig --out-kubeconfig="${KINK_KUBECONFIG}"

    cat "${KINK_KUBECONFIG}"

    if [ -z "${KINK_IT_NO_CLEANUP}" ]; then
        helm delete wordpress
        "${KINK_COMMAND[@]}" delete cluster
    fi
done
