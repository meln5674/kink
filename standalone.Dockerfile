ARG BASE_IMAGE=docker.io/library/debian:bullseye-slim

ARG DOCKER_IMAGE=docker.io/library/docker:20

FROM ${DOCKER_IMAGE} AS docker

FROM ${BASE_IMAGE} AS download

RUN apt-get update && apt-get install -y curl iptables buildah

FROM download as etcd

ARG ETCD_VERSION=v3.5.4
# GOOGLE_URL=https://storage.googleapis.com/etcd
# GITHUB_URL=https://github.com/etcd-io/etcd/releases/download
ARG ETCD_URL=https://github.com/etcd-io/etcd/releases/download
RUN mkdir /tmp/etcd-download-test \
 && curl -fvL ${ETCD_URL}/${ETCD_VERSION}/etcd-${ETCD_VERSION}-linux-amd64.tar.gz \
  | tar xzv -C /tmp/etcd-download-test --strip-components=1 \
 && mv /tmp/etcd-download-test/etcdctl /usr/local/bin/

FROM download as yq

ARG YQ_VERSION=v4.28.2
RUN curl -fvL "https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/yq_linux_amd64" > /usr/local/bin/yq \
 && chmod +x /usr/local/bin/yq

FROM download AS helm

ARG HELM_VERSION=v3.10.1

RUN curl -fvL https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz \
  | tar xz -C /usr/local/bin/ --strip-components=1 linux-amd64/helm

FROM download AS kink

ARG K8S_VERSION=v1.25.3

ARG K3S_PATCH_NUMBER=1
ARG K3S_VERSION=${K8S_VERSION}+k3s${K3S_PATCH_NUMBER}
ARG ARCH= #amd64 # amd64 is the default, not needed
ARG K3S_URL=

RUN K3S_FILENAME="k3s$([ -n "${ARCH}" ] && echo "-${ARCH}" ; exit 0 )" \
 && K3S_URL=${K3S_URL:-https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}/${K3S_FILENAME}} \
 && echo "${K3S_URL}" \
 && curl -fvL "${K3S_URL}" > /usr/local/bin/k3s \
 && chmod 755 /usr/local/bin/k3s

ARG RKE2_PATCH_NUMBER=1
ARG INSTALL_RKE2_VERSION=${K8S_VERSION}+rke2r${RKE2_PATCH_NUMBER}
RUN  curl -fvL https://get.rke2.io/ \
  | INSTALL_RKE2_SKIP_RELOAD=1 sh -

ARG KUBECTL_MIRROR=https://dl.k8s.io/release
ARG KUBECTL_VERSION=${K8S_VERSION}
ARG KUBECTL_URL=${KUBECTL_MIRROR}/${KUBECTL_VERSION}/bin/linux/amd64/kubectl

RUN curl -fvL "${KUBECTL_URL}" > /usr/local/bin/kubectl \
 && chmod +x /usr/local/bin/kubectl

COPY --from=docker /usr/local/bin/docker /usr/local/bin/docker
COPY --from=helm /usr/local/bin/helm /usr/local/bin/helm
COPY --from=yq /usr/local/bin/yq /usr/local/bin/yq
COPY --from=etcd /usr/local/bin/etcdctl /usr/local/bin/etcdctl

COPY charts/local-path-provisioner.yaml /etc/kink/extra-manifests/rke2/system/kink-local-path-provisioner.yaml
COPY charts/shared-local-path-provisioner.yaml /etc/kink/extra-manifests/rke2/system/kink-shared-local-path-provisioner.yaml
COPY charts/shared-local-path-provisioner.yaml /etc/kink/extra-manifests/k3s/system/kink-shared-local-path-provisioner.yaml
RUN mkdir -p /etc/kink/extra-manifests/k3s/user /etc/kink/extra-manifests/rke2/user


COPY bin/kink /usr/local/bin/kink

VOLUME /var/lib/rancher/
VOLUME /var/lib/kubelet
VOLUME /etc/rancher

ENTRYPOINT ["/usr/local/bin/k3s"]