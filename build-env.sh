#!/bin/bash -xe

SCRIPTPATH="$( cd -- "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"

# Assume the user wants an interactive shell if no args are passed, otherwise, run the provided command
if [ "$#" -eq 0 ]; then
    COMMAND=( bash )
else
    COMMAND=( "$@" )
fi

export DOCKER_BUILDKIT=1

IMAGE_REPO=${IMAGE_REPO:-localhost/meln5674/kink/build-env}

IMAGE_TAG=${IMAGE_TAG:-$(md5sum build-env.Dockerfile | awk '{ print $1 }')}

IMAGE="${IMAGE_REPO}:${IMAGE_TAG}"


if [ -z "${NO_BUILD_IMAGE}" ]; then
    docker build -f "${SCRIPTPATH}/build-env.Dockerfile" -t "${IMAGE}" "${SCRIPTPATH}"
fi

DOCKER_RUN_ARGS=(
    --rm
    -i
)

if [ -t 0 ] ; then
    DOCKER_RUN_ARGS+=( -t )
fi

# Make it look like we're in the same directory as we ran from
DOCKER_RUN_ARGS+=(
    -v "/${PWD}:/${PWD}"
    -w "/${PWD}"
)

# Make it look like we're the same user
DOCKER_RUN_ARGS+=(
    -u "$(id -u):$(id -g)"
    -v "/${HOME}:/${HOME}"
    -v /etc/passwd:/etc/passwd
    -v /etc/group:/etc/group
    -e HOME
)

for group in $(id -G); do
    DOCKER_RUN_ARGS+=( --group-add "${group}" )
done

# Provide access to docker
DOCKER_RUN_ARGS+=(
    -v /var/run/docker.sock:/var/run/docker.sock
)

# Provide access to an existing kind cluster, as well as enable port-forwarding for live dev env
DOCKER_RUN_ARGS+=(
    -e KUBECONFIG
    --network host
)

# If GOPATH is set, also mount it and forward the env so we can re-use the package cache
if [ -n "${GOPATH}" ]; then
    DOCKER_RUN_ARGS+=(
        -e GOPATH
        -v "/${GOPATH}:/${GOPATH}"
    )
fi

# Forward variables used by the integration test scripts
DOCKER_RUN_ARGS+=(
    -e KINK_IT_DEV_MODE
    -e KINK_IT_NO_CLEANUP
    -e KIND_CLUSTER_NAME
)

exec docker run "${DOCKER_RUN_ARGS[@]}" ${EXTRA_BUILD_ENV_ARGS} "${IMAGE}" "${COMMAND[@]}"
