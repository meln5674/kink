You've heard of [Kubernetes in Docker (KinD)](https://github.com/kubernetes-sigs/kind), now get ready for

# Kubernetes in Kubernetes (KinK)

Deploy a Kubernetes cluster inside of another Kubernetes cluster, with all of the HA and scalability you would expect.

## Why?

Even if you have access to a Kubernetes cluster, odds are, you don't have cluster admin permissions to it, which means you cannot do things like install custom resource defintions (CRDs). This makes testing custom operators and other tools that manage Kubernetes itself a pain. When deploying an application in Kubernetes as part of a CI/CD pipeline integration test or staging environment, there may be extra state floating around that may interfere with or even invalidate the results of your tests. Using KinK, you can create a completely fresh cluster each time, just like using KinD, but in a Kubernetes cluster, leveraging as much scalability as the parent cluster affords, and usable from within another Pod.

## How?

KinK is based off of [k3s](https://k3s.io/), a super-lightweight Kubernetes distribution, which includes networking, storage, and ingress.

## Getting Started

To start, build the base image, and push it to a registry available to your cluster

```bash
docker build -t <image registry>:<image name>:<image tag> .
docker push <image registry>/<image name>:<image tag>
```

Next, use [Helm](https://helm.sh/) to deploy a new cluster into your existing cluster

```bash
kubectl create secret generic <token secret> --from-literal token=<some very secret value>

helm upgrade --install kink helm/kink \
    # Set to the image you built
    --set image.repository=<image registry>/<image name> \
    --set image.tag=<image tag> \
    # Add the secret you created before
    --set token.existingSecret.name=<token secret> \
    # Enable peristence
    --set controlplane.persistence.enabled=true \
    --set workers.persistence.enabled=true \
    # Necessary if your cluster is not running rootless
    --set controlplane.securityContext.privileged=true \
    --set worker.securityContext.privileged=true \
    # To enable HA
    --set controlplane.replicaCount=3 \
    --set worker.replicaCount=3
    # See values.yaml for all fields you can set
```

Finally, pull out the generated KUBECONFIG file, port-forward to the new API server, and start using it!
```bash
kubectl cp kink-controlplane-0:/etc/rancher/k3s/k3s.yaml ./kink.kubeconfig
kubectl port-forward svc/kink-controlplane 6443:6443 &
kubectl --kubeconfig=kink.kubeconfig version
kubectl --kubeconfig=kink.kubeconfig cluster-info
kubectl --kubeconfig=kink.kubeconfig get nodes
kubectl --kubeconfig=kink.kubeconfig get all -A
```

Once you're done, throw it away
```bash
helm delete kink
```

If you don't have a cluster to test with, you can use [KinD](https://github.com/kubernetes-sigs/kind) to create a cluster in a single docker container, and the nesting will work as you expect.

```bash
kind create cluster --kubeconfig=./kind.kubeconfig
kind load docker-image <image registry>/<image name>:<image tag>
helm --kubeconfig=./kind.kubeconfig ...
# ...
kubectl --kubeconfig=./kind.kubeconfig ...
# ...
kubectl --kubeconfig=./kink.kubeconfig ...
```
