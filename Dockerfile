ARG BASE_IMAGE=debian:bullseye-slim

FROM ${BASE_IMAGE}

RUN apt-get update && apt-get install -y curl

ARG K3S_VERSION=v1.25.2+k3s1
ARG ARCH= #amd64 # amd64 is the default, not needed
ARG K3S_URL=

RUN K3S_FILENAME="k3s$([ -n "${ARCH}" ] && echo "-${ARCH}" ; exit 0 )" \
 && K3S_URL=${K3S_URL:-https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}/${K3S_FILENAME}} \
 && echo "${K3S_URL}" \
 && curl -fvL "${K3S_URL}" > /usr/local/bin/k3s \
 && chmod 755 /usr/local/bin/k3s

VOLUME /var/lib/rancher/k3s
VOLUME /var/lib/kubelet
VOLUME /etc/rancher

ENTRYPOINT ["/usr/local/bin/k3s"]
