ARG PROXY_CACHE=

ARG GO_IMAGE_REPO=docker.io/library/golang
ARG GO_IMAGE_TAG=1.18

FROM ${PROXY_CACHE}${GO_IMAGE_REPO}:${GO_IMAGE_TAG} as base

FROM base AS docker

ARG DOCKER_MIRROR=https://download.docker.com/linux/static/stable/x86_64
ARG DOCKER_VERSION=20.10.9
ARG DOCKER_URL=${DOCKER_MIRROR}/docker-${DOCKER_VERSION}.tgz

RUN curl -fvL "${DOCKER_URL}" \
  | tar xz -C /usr/local/bin --strip-components=1 docker

FROM base AS kubectl

ARG KUBECTL_MIRROR=https://dl.k8s.io/release
ARG KUBECTL_VERSION=v1.25.3
ARG KUBECTL_URL=${KUBECTL_MIRROR}/${KUBECTL_VERSION}/bin/linux/amd64/kubectl

RUN curl -fvL "${KUBECTL_URL}" > /usr/local/bin/kubectl \
 && chmod +x /usr/local/bin/kubectl

FROM base AS helm

ARG HELM_MIRROR=https://get.helm.sh
ARG HELM_VERSION=v3.10.1
ARG HELM_URL=${HELM_MIRROR}/helm-${HELM_VERSION}-linux-amd64.tar.gz

RUN curl -fvL "${HELM_URL}" \
  | tar xz -C /usr/local/bin/ --strip-components=1 linux-amd64/helm

FROM base AS kind

ARG KIND_MIRROR=https://github.com/kubernetes-sigs/kind/releases/download
ARG KIND_VERSION=v0.17.0
ARG KIND_URL=${KIND_MIRROR}/${KIND_VERSION}/kind-linux-amd64

RUN curl -fvL "${KIND_URL}" > /usr/local/bin/kind \
 && chmod +x /usr/local/bin/kind

FROM base AS goinstall

COPY go.mod /go.mod
RUN go install $(cat /go.mod | grep ginkgo | awk '{ print $1 "/ginkgo@" $2 }') && rm /go.mod
RUN go install github.com/meln5674/helm-hog@latest 

FROM base

ARG INSTALL_BUILDAH="apt-get update -y && apt-get -y install buildah"
RUN bash -c "${INSTALL_BUILDAH}"

COPY --from=docker /usr/local/bin/docker /usr/local/bin/docker
COPY --from=kubectl /usr/local/bin/kubectl /usr/local/bin/kubectl
COPY --from=helm /usr/local/bin/helm /usr/local/bin/helm
COPY --from=kind /usr/local/bin/kind /usr/local/bin/kind
COPY --from=goinstall /go /go

VOLUME /go/src/github.com/meln5674/kink
