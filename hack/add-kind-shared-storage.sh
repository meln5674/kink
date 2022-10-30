#!/bin/bash -xe

CLUSTER_NAME=$1
STORAGE_PATH=$2
RELEASE=$3
CHART=$4
NAMESPACE=$5
STORAGE_CLASS_NAME=$6

LAST_VAR=${STORAGE_CLASS_NAME}

if [ "${CLUSTER_NAME}" == "-h" ] || [ "${CLUSTER_NAME}" == "--help" ] || ([ "${CLUSTER_NAME}" == "help" ] && [ -z "${STORAGE_PATH}" ]) || [ -z "${LAST_VAR}" ]; then
    echo "USAGE: add-kink-shared-storage.sh CLUSTER_NAME STORAGE_PATH RELEASE CHART"
    exit 1
fi

set -u

kind get nodes --name="${CLUSTER_NAME}" | while read -r node; do
    docker exec "${node}" mkdir -p "${STORAGE_PATH}"
done

export KUBECONFIG=$(mktemp)
trap "rm ${KUBECONFIG}" EXIT

kind export kubeconfig --kubeconfig="${KUBECONFIG}" --name="${CLUSTER_NAME}"

helm upgrade --install "${RELEASE}" "${CHART}" \
    --namespace "${NAMESPACE}" \
    --set storageClass.name="${STORAGE_CLASS_NAME}" \
    --set nodePathMap='null' \
    --set sharedFileSystemPath="${STORAGE_PATH}" \
    --set fullnameOverride="${RELEASE}" \
    --set configmap.name="${RELEASE}"
