#!/bin/bash -xe

which curl || (echo "curl not on PATH" ; exit 1)
which go || (echo "go not on PATH" ; exit 1)
which helm || (echo "helm not on PATH" ; exit 1)
which docker || (echo "docker not on PATH" ; exit 1)
which buildah || (echo "buildah not on PATH" ; exit 1)
which kind || (echo "kind not on PATH" ; exit 1)
which kubectl || (echo "kubectl not on PATH" ; exit 1)

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
if [ -z "${CLUSTER_EXISTS}" ] || ([ -n "${CLUSTER_EXISTS}" ] && [ -z "${KINK_IT_NO_CLEANUP}" ] && [ -z "${KINK_IT_CLEANUP}" ]); then
    kind create cluster \
        --name="${KIND_CLUSTER_NAME}" \
        --kubeconfig="${KIND_KUBECONFIG}"
fi

export KUBECONFIG="${KIND_KUBECONFIG}"

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



kubectl get pods -w &
GET_PODS_PID=$!
TRAP_CMD="kill ${GET_PODS_PID} ; ${TRAP_CMD}"
trap "${TRAP_CMD}" EXIT


for test_case in ${TEST_CASES:-k3s k3s-ha rke2}; do
    TEST_CASE="${test_case}" \
    IMAGE_REPO="${IMAGE_REPO}" \
    IMAGE_TAG="${IMAGE_TAG}" \
    KUBECONFIG="${KIND_KUBECONFIG}" \
    KINK_IT_NO_KINK_CREATE="${KINK_IT_NO_KINK_CREATE}" \
    KINK_IT_NO_LOAD="${KINK_IT_NO_LOAD}" \
    KINK_IT_NO_CLEANUP="${KINK_IT_NO_CLEANUP}" \
    integration-test/run-case.sh 
done
