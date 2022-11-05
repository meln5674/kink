#!/bin/bash -xeu

which helm || (echo "helm not on PATH" ; exit 1)
which kubectl || (echo "kubectl not on PATH" ; exit 1)

TRAP_CMD=''

(set +e ; helm upgrade --install "kink-test-${TEST_CASE}" helm/kink-test \
    --set testCase="${TEST_CASE}" \
    --set image.repository="${IMAGE_REPO}" \
    --set image.tag="${IMAGE_TAG}" \
    --set noKinkCreate="${KINK_IT_NO_KINK_CREATE}" \
    --set noLoad="${KINK_IT_NO_LOAD}" \
    --set noCleanup="${KINK_IT_NO_CLEANUP}" \
    --wait ) &
UPGRADE_PID=$!

if [ -z "${KINK_IT_NO_CLEANUP}" ]; then
    TRAP_CMD=$(cat <<'EOF'

    helm delete "kink-test-${TEST_CASE}" --wait &
    DELETE_PID=$!

    kubectl logs -f "job/kink-test-${TEST_CASE}-cleanup"

    wait "${DELETE_PID}"

    "${TRAP_CMD}"

    exit 0
EOF
)
    trap "set +e ; ${TRAP_CMD}" EXIT
fi

kubectl get pods -o wide -w &
GET_PODS_PID=$!
TRAP_CMD="kill ${GET_PODS_PID} ; ${TRAP_CMD}"
trap "set +e ; ${TRAP_CMD}" EXIT

sleep 20

kubectl logs -f "job/kink-test-${TEST_CASE}"

wait "${UPGRADE_PID}"

echo "${TEST_CASE} Passed!"
