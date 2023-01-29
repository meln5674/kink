#!/bin/bash -xe

set -o pipefail

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

KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME=kink-it}
CLUSTER_EXISTS="$(
    if kind get clusters | grep -qw "${KIND_CLUSTER_NAME}" ; then
        echo 1
    fi
)"

KIND_KUBECONFIG=./integration-test/kind.kubeconfig
if [ -z "${CLUSTER_EXISTS}" ] || ([ -n "${CLUSTER_EXISTS}" ] && [ -z "${KINK_IT_NO_CLEANUP}" ] && [ -z "${KINK_IT_CLEANUP}" ]); then
    KIND_CONFIG_FILE=integration-test/kind.config.yaml
    cat "${KIND_CONFIG_FILE}.tpl" | sed 's|\${PWD}|'"${PWD}"'|g' | tee "${KIND_CONFIG_FILE}"
    kind create cluster \
        --name="${KIND_CLUSTER_NAME}" \
        --kubeconfig="${KIND_KUBECONFIG}" \
        --config="${KIND_CONFIG_FILE}"
fi

if [ -z "${KINK_IT_CLEANUP}" ]; then
    hack/add-kind-shared-storage.sh \
        "${KIND_CLUSTER_NAME}" \
        /var/shared-local-path-provisioner \
        shared-local-path-provisioner \
        charts/local-path-provisioner-0.0.24-dev.tgz \
        kube-system \
        shared-local-path
fi

export KUBECONFIG="${KIND_KUBECONFIG}"

if [ -z "${KINK_IT_NO_CLEANUP}" ]; then
    # TODO: Use a temporary root container instead of sudo here
    TRAP_CMD="kind delete cluster --name='${KIND_CLUSTER_NAME}' ; docker run --rm -v "${PWD}/integration-test/:/tmp/integration-test" centos:7 rm -rf /tmp/integration-test/local-path-provisioner /tmp/integration-test/shared-local-path-provisioner"
    trap "set +e; ${TRAP_CMD}" EXIT
fi

if [ -z "${KINK_IT_CLEANUP}" ] ; then
    kind load docker-image "${BUILT_IMAGE}" --name="${KIND_CLUSTER_NAME}"
fi

if [ -n "${KINK_IT_CLEANUP}" ]; then
    export KUBECONFIG="${KIND_KUBECONFIG}"
    for test_case in k3s k3s-ha rke2; do
        KINK_CLUSTER_NAME=it-${test_case}
        KINK_COMMAND=( bin/kink.cover --config "${KINK_CONFIG_FILE}" --name "${KINK_CLUSTER_NAME}" -v11 )
        "${KINK_COMMAND[@]}" delete cluster --name="${KINK_CLUSTER_NAME}" || true
    done
    for test_case in k3s k3s-ha rke2; do
        helm delete "kink-test-${test_case}" --wait &
        DELETE_PID=$!

        kubectl logs -f "job/kink-test-${test_case}-cleanup" || true

        wait "${DELETE_PID}" || true

    done
    kind delete cluster --name="${KIND_CLUSTER_NAME}"
    exit 0
fi


# We have to tail /dev/null here so that even if this process exits, the trap kill doesn't fail
(set +e ; kubectl get pods -o wide -w ; tail -f /dev/null ) &
GET_PODS_PID=$!
TRAP_CMD="kill ${GET_PODS_PID} ; ${TRAP_CMD}"
trap "set +e; ${TRAP_CMD}" EXIT

export KUBECONFIG="${KIND_KUBECONFIG}"

# helm upgrade --install -n 

ALL_TEST_CASES='k3s k3s-single k3s-ha rke2'

for test_case in ${TEST_CASES:-${ALL_TEST_CASES}}; do
    TEST_CASE="${test_case}" \
    IMAGE_REPO="${IMAGE_REPO}" \
    IMAGE_TAG="${IMAGE_TAG}" \
    KINK_IT_NO_KINK_CREATE="${KINK_IT_NO_KINK_CREATE}" \
    KINK_IT_NO_LOAD="${KINK_IT_NO_LOAD}" \
    KINK_IT_NO_CLEANUP="${KINK_IT_NO_CLEANUP}" \
    integration-test/run-case.sh 
done

if [ -n "$( sed 's/\s//' <<< "${TEST_CASES}" )" ]; then
    echo All out-of-cluster tests Passed!
else
    echo No out-of-cluster-tests were run...
fi

for test_case in ${IN_CLUSTER_TEST_CASES:-${ALL_TEST_CASES}}; do
    TEST_CASE="${test_case}" \
    IMAGE_REPO="${IMAGE_REPO}" \
    IMAGE_TAG="${IMAGE_TAG}" \
    KINK_IT_NO_KINK_CREATE="${KINK_IT_NO_KINK_CREATE}" \
    KINK_IT_NO_LOAD="${KINK_IT_NO_LOAD}" \
    KINK_IT_NO_CLEANUP="${KINK_IT_NO_CLEANUP}" \
    integration-test/run-case-in-cluster.sh 
done


if [ -n "$( sed 's/\s//' <<< "${IN_CLUSTER_TEST_CASES}" )" ]; then
    echo All in-cluster tests Passed!
else
    echo No in-cluster-tests were run...
fi
