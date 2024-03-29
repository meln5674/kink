#!/bin/bash -xeu

which curl || (echo "curl not on PATH" ; exit 1)
which helm || (echo "helm not on PATH" ; exit 1)
which docker || (echo "docker not on PATH" ; exit 1)
which buildah || (echo "buildah not on PATH" ; exit 1)
which kubectl || (echo "kubectl not on PATH" ; exit 1)

TRAP_CMD=''

KINK_CLUSTER_NAME=it-${TEST_CASE}
KINK_CONFIG_FILE=integration-test/kink.${TEST_CASE}.config.yaml

KINK_COMMAND=( bin/kink.cover --config "${KINK_CONFIG_FILE}" --name "${KINK_CLUSTER_NAME}" -v11 )

KINK_KUBECONFIG=integration-test/kink-${TEST_CASE}.kubeconfig

if ps aux | grep port-forward | grep 6443 ; then
    echo '!!! SOMETHING ELSE IS STILL LISTENING'
    sleep 30
fi

if ! ("${KINK_COMMAND[@]}" get cluster | tee /dev/stderr | grep -w "${KINK_CLUSTER_NAME}") || [ -z "${KINK_IT_NO_KINK_CREATE}" ]; then
    "${KINK_COMMAND[@]}" create cluster \
        --chart ./helm/kink \
        --set image.repository="${IMAGE_REPO}" \
        --set image.tag="${IMAGE_TAG}" \
        --out-kubeconfig="${KINK_KUBECONFIG}"
    
    sleep 15
fi

if [ -z "${KINK_IT_NO_CLEANUP}" ]; then
    TRAP_CMD="${KINK_COMMAND[@]} delete cluster; ${TRAP_CMD}"
    trap "set +e; ${TRAP_CMD}" EXIT
fi

"${KINK_COMMAND[@]}" sh -- '
    set -xe
    while ! kubectl version ; do
        sleep 10;
    done
    kubectl cluster-info
    while kubectl get nodes | tee /dev/stderr | grep NotReady; do
        echo 'Not all nodes are ready yet'
        sleep 15
    done
'


WORDPRESS_IMAGE=docker.io/bitnami/wordpress:6.0.3-debian-11-r3
MARIADB_IMAGE=docker.io/bitnami/mariadb:10.6.10-debian-11-r6
MEMCACHED_IMAGE=docker.io/bitnami/memcached:1.6.17-debian-11-r15

LOAD_FLAGS=

if [ -e "integration-test/kink.${TEST_CASE}.load-flags" ]; then
    LOAD_FLAGS=$(cat "integration-test/kink.${TEST_CASE}.load-flags")
fi

if [ -z "${KINK_IT_NO_LOAD}" ]; then 
    docker pull "${WORDPRESS_IMAGE}" 
    "${KINK_COMMAND[@]}" ${LOAD_FLAGS} load ${LOAD_FLAGS} docker-image \
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
if [ -z "${KINK_IT_NO_CLEANUP}" ]; then
    TRAP_CMD="${KINK_COMMAND[@]} sh -- 'if helm get values wordpress; then  helm delete wordpress fi' ; ${TRAP_CMD}"
    trap "set +e; ${TRAP_CMD}" EXIT
fi



"${KINK_COMMAND[@]}" sh --exported-kubeconfig="${KINK_KUBECONFIG}" -- '

    WORDPRESS_CHART_VERSION=15.2.7

    set -xe
    while ! kubectl version ; do
        sleep 1;
    done
    kubectl cluster-info
    kubectl get nodes
        
    (set +e ; kubectl get pods -o wide -w ; tail -f /dev/null) &
    GET_PODS_PID=$!
    TRAP_CMD="kill ${GET_PODS_PID} ; wait ${GET_PODS_PID} ; ${TRAP_CMD}"
    trap "set +e; ${TRAP_CMD}" EXIT

    helm upgrade --install wordpress bitnami/wordpress \
        --version "${WORDPRESS_CHART_VERSION}" \
        --wait \
        $(cat "integration-test/wordpress.${TEST_CASE}.flags") \
        --debug

    kubectl get all -A
'

"${KINK_COMMAND[@]}" export kubeconfig --out-kubeconfig="${KINK_KUBECONFIG}"

cat "${KINK_KUBECONFIG}"

(set +e ; "${KINK_COMMAND[@]}" port-forward ; tail -f /dev/null ) &
KINK_PORT_FORWARD_PID=$!
TRAP_CMD="kill ${KINK_PORT_FORWARD_PID} ; wait ${KINK_PORT_FORWARD_PID} ; ${TRAP_CMD}"
while ! kubectl version --kubeconfig="${KINK_KUBECONFIG}" ; do
    sleep 1;
done
helm list --kubeconfig="${KINK_KUBECONFIG}" 
kubectl get svc -A --kubeconfig="${KINK_KUBECONFIG}"
(set +e ; kubectl port-forward svc/wordpress 8080:80 --kubeconfig="${KINK_KUBECONFIG}" ; tail -f /dev/null) &
PORT_FORWARD_PID=$!
TRAP_CMD="kill ${PORT_FORWARD_PID} ; wait ${PORT_FORWARD_PID} ; ${TRAP_CMD}"
trap "set +e; ${TRAP_CMD}" EXIT
sleep 10
curl -v http://localhost:8080


echo "${TEST_CASE}" Passed!
