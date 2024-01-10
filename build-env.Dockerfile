ARG PROXY_CACHE=

ARG GO_IMAGE_REPO=docker.io/library/golang
ARG GO_IMAGE_TAG=1.21

FROM ${PROXY_CACHE}${GO_IMAGE_REPO}:${GO_IMAGE_TAG} AS base

WORKDIR /make-env
COPY make-env.Makefile make-env.yaml ./

ARG DOCKER_MIRROR=https://download.docker.com/linux/static/stable/x86_64
ARG DOCKER_VERSION=20.10.9
ARG DOCKER_URL=${DOCKER_MIRROR}/docker-${DOCKER_VERSION}.tgz

RUN curl -fvL "${DOCKER_URL}" \
  | tar xz -C /usr/local/bin --strip-components=1 docker

RUN make -j -f make-env.Makefile test-tools k8s-tools SHELL=/bin/bash

ENV LOCALBIN=/make-env/bin
ENV PATH=/make-env/bin:${PATH}

WORKDIR /go/src/github.com/meln5674/kink

VOLUME /go/src/github.com/meln5674/kink
