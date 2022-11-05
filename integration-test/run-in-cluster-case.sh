#!/bin/bash -xeu

# This is the part that runs in-cluster, see run-in-cluster-case.sh for the part that runs this

which curl || (echo "curl not on PATH" ; exit 1)
which helm || (echo "helm not on PATH" ; exit 1)
which docker || (echo "docker not on PATH" ; exit 1)
which buildah || (echo "buildah not on PATH" ; exit 1)
which kubectl || (echo "kubectl not on PATH" ; exit 1)

TRAP_CMD=''

KINK_CLUSTER_NAME=it-${TEST_CASE}
KINK_CONFIG_FILE=integration-test/kink.${TEST_CASE}.config.yaml

KINK_COMMAND=( bin/kink.cover --config "${KINK_CONFIG_FILE}" --name "${KINK_CLUSTER_NAME}" -v11)

KINK_KUBECONFIG=integration-test/kink-${TEST_CASE}.kubeconfig

INNER_KUBECTL=( kubectl --kubeconfig="${KINK_KUBECONFIG}" )
INNER_HELM=( helm --kubeconfig="${KINK_KUBECONFIG}" )

if ! ("${KINK_COMMAND[@]}" get cluster | tee /dev/stderr | grep -w "${KINK_CLUSTER_NAME}") || [ -z "${KINK_IT_NO_KINK_CREATE}" ]; then
    "${KINK_COMMAND[@]}" create cluster \
        --set image.repository="${IMAGE_REPO}" \
        --set image.tag="${IMAGE_TAG}" \
        --out-kubeconfig="${KINK_KUBECONFIG}"
fi

if [ -z "${KINK_IT_NO_CLEANUP}" ]; then
    TRAP_CMD="${KINK_COMMAND[@]} delete cluster; ${TRAP_CMD}"
    trap "set +e; ${TRAP_CMD}" EXIT
fi

"${INNER_KUBECTL[@]}" config use-context "in-cluster"

while ! "${INNER_KUBECTL[@]}" version ; do
    sleep 10;
done
"${INNER_KUBECTL[@]}" cluster-info
"${INNER_KUBECTL[@]}" get nodes

WORDPRESS_CHART_VERSION=15.2.7

WORDPRESS_IMAGE=docker.io/bitnami/wordpress:6.0.3-debian-11-r3
MARIADB_IMAGE=docker.io/bitnami/mariadb:10.6.10-debian-11-r6
MEMCACHED_IMAGE=docker.io/bitnami/memcached:1.6.17-debian-11-r15

LOAD_FLAGS=

if [ -e "integration-test/kink.${TEST_CASE}.load-flags" ]; then
    LOAD_FLAGS=$(cat "integration-test/kink.${TEST_CASE}.load-flags")
fi

if [ -z "${KINK_IT_NO_LOAD}" ]; then 
    docker pull "${WORDPRESS_IMAGE}" 
    "${KINK_COMMAND[@]}" load ${LOAD_FLAGS} docker-image \
        --config "${KINK_CONFIG_FILE}" \
        --name "${KINK_CLUSTER_NAME}" \
        --image "${WORDPRESS_IMAGE}" \
        --parallel-loads=1

    docker pull "${MARIADB_IMAGE}"
    docker save "${MARIADB_IMAGE}" > ./integration-test/mariadb.tar
    "${KINK_COMMAND[@]}" load ${LOAD_FLAGS} docker-archive --archive ./integration-test/mariadb.tar

    buildah build-using-dockerfile \
        --file - \
        --tag "${MEMCACHED_IMAGE}" \
        <<< "FROM ${MEMCACHED_IMAGE}"
    buildah push "${MEMCACHED_IMAGE}" "oci-archive:./integration-test/memcached-image.tar:${MEMCACHED_IMAGE}"
    "${KINK_COMMAND[@]}" load ${LOAD_FLAGS} oci-archive --archive ./integration-test/memcached-image.tar
fi


helm repo add bitnami https://charts.bitnami.com/bitnami

"${INNER_KUBECTL[@]}" get pods -o wide -w &
GET_PODS_PID=$!
TRAP_CMD="kill ${GET_PODS_PID} ; ${TRAP_CMD}"
trap "set +e; ${TRAP_CMD}" EXIT

"${INNER_HELM[@]}" upgrade --install wordpress bitnami/wordpress \
    --version "${WORDPRESS_CHART_VERSION}" \
    --wait \
    $(cat "integration-test/wordpress.${TEST_CASE}.flags") \
    --debug

if [ -z "${KINK_IT_NO_CLEANUP}" ]; then
    TRAP_CMD="${KINK_COMMAND[@]} exec -- "${INNER_HELM[@]}" delete wordpress ; ${TRAP_CMD}"
    trap "set +e; ${TRAP_CMD}" EXIT
fi

"${INNER_KUBECTL[@]}" get all -A
"${INNER_KUBECTL[@]}" port-forward svc/wordpress 8080:80 &

"${INNER_KUBECTL[@]}" logs deploy/wordpress

PORT_FORWARD_PID=$!
TRAP_CMD="kill ${PORT_FORWARD_PID} ; ${TRAP_CMD}"
trap "set +e; ${TRAP_CMD}" EXIT
sleep 5
curl -v http://localhost:8080
