ARG BASE_IMAGE=docker.io/library/debian:bullseye-slim

ARG GO_IMAGE=docker.io/library/golang:1.18

ARG DOCKER_IMAGE=docker.io/library/docker:20

FROM ${DOCKER_IMAGE} AS docker

FROM ${GO_IMAGE} AS go

WORKDIR /src/kink/

COPY main.go go.mod go.sum /src/kink/
COPY cmd /src/kink/cmd
COPY pkg /src/kink/pkg

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -tags netgo -ldflags '-w -extldflags "-static"' -o bin/kink main.go

FROM ${BASE_IMAGE}

RUN apt-get update && apt-get install -y curl iptables buildah

ARG K3S_VERSION=v1.25.2+k3s1
ARG ARCH= #amd64 # amd64 is the default, not needed
ARG K3S_URL=

RUN K3S_FILENAME="k3s$([ -n "${ARCH}" ] && echo "-${ARCH}" ; exit 0 )" \
 && K3S_URL=${K3S_URL:-https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}/${K3S_FILENAME}} \
 && echo "${K3S_URL}" \
 && curl -fvL "${K3S_URL}" > /usr/local/bin/k3s \
 && chmod 755 /usr/local/bin/k3s

ARG INSTALL_RKE2_VERSION=v1.25.3+rke2r1
RUN  curl -fvL https://get.rke2.io/ \
  | INSTALL_RKE2_SKIP_RELOAD=1 sh -

ARG ETCD_VERSION=v3.5.4
# GOOGLE_URL=https://storage.googleapis.com/etcd
# GITHUB_URL=https://github.com/etcd-io/etcd/releases/download
ARG ETCD_URL=https://github.com/etcd-io/etcd/releases/download
RUN mkdir /tmp/etcd-download-test \
 && curl -fvL ${ETCD_URL}/${ETCD_VERSION}/etcd-${ETCD_VERSION}-linux-amd64.tar.gz \
  | tar xzv -C /tmp/etcd-download-test --strip-components=1 \
 && mv /tmp/etcd-download-test/etcdctl /usr/local/bin/

ARG YQ_VERSION=v4.28.2
RUN curl -fvL "https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/yq_linux_amd64" > /usr/local/bin/yq \
 && chmod +x /usr/local/bin/yq

COPY charts/local-path-provisioner.yaml /etc/kink/extra-manifests/rke2/system/kink-local-path-provisioner.yaml
COPY charts/shared-local-path-provisioner.yaml /etc/kink/extra-manifests/rke2/system/kink-shared-local-path-provisioner.yaml
COPY charts/shared-local-path-provisioner.yaml /etc/kink/extra-manifests/k3s/system/kink-shared-local-path-provisioner.yaml
RUN mkdir -p /etc/kink/extra-manifests/k3s/user /etc/kink/extra-manifests/rke2/user

COPY --from=go /src/kink/bin/kink /usr/local/bin/kink

COPY --from=docker /usr/local/bin/docker /usr/local/bin/docker

ARG HELM_VERSION=v3.10.1

RUN curl -fvL https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz \
  | tar xz -C /usr/local/bin/ --strip-components=1 linux-amd64/helm


VOLUME /var/lib/rancher/
VOLUME /var/lib/kubelet
VOLUME /etc/rancher

ENTRYPOINT ["/usr/local/bin/k3s"]
