You've heard of [Kubernetes in Docker (KinD)](https://github.com/kubernetes-sigs/kind), now get ready for

# Kubernetes in Kubernetes (KinK)

## What?

Deploy a Kubernetes cluster inside of another Kubernetes cluster, with all of the HA and scalability you would expect.

Kink is currently made up of two components:
* A Helm Chart which deploys a nested Cluster
* A CLI tool to automate using the above chart, as well as normal tasks like load images, and providing access to clusters which aren't otherwise exposed

## Why?

Even if you have access to a Kubernetes cluster, odds are, you don't have cluster admin permissions to it, which means you cannot do things like install custom resource defintions (CRDs). This makes testing custom operators and other tools that manage Kubernetes itself a pain. When deploying an application in Kubernetes as part of a CI/CD pipeline integration test or staging environment, there may be extra state floating around that may interfere with or even invalidate the results of your tests. Using KinK, you can create a completely fresh cluster each time, just like using KinD, but in a Kubernetes cluster, leveraging as much scalability as the parent cluster affords, and usable from within another Pod.

While it is entirely possible to use a cloud provider like AWS, GCP, or Azure to create a new cluster as part of a CI/CD pipeline, this is both insecure and wasteful, as it requires providing your developers the ability to create (and more importantly, delete!) all of the resources required to do so, and requires you to pay for those resources, even if you aren't fully utilizing them. By creating a new nested cluster inside of your existing software factory cluster, you can place more restrictions on what permissions developers require, and leverage your existing factory autoscaling to maximize resource utilization and descrease costs.

## How?

KinK is based off of [k3s](https://k3s.io/), a super-lightweight Kubernetes distribution, which includes networking, storage, and ingress.

## Getting Started

To start, build the base image, and push it to a registry available to your cluster

```bash
docker build -t <image registry>:<image name>:<image tag> .
docker push <image registry>/<image name>:<image tag>
```

Next, install the CLI interface

```bash
go install github.com/meln5674/kink
```

You'll now be able to deploy your cluster

```bash
kink create cluster \
    --chart ./helm/kink \
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
    # To enable multiple workers
    --set worker.replicaCount=3
    # See values.yaml for all fields you can set
```

Finally, start a nested shell configured to access your cluster, and start using it!
```bash
kink sh
# New shell will open
kubectl version
kubectl cluster-info
kubectl get nodes
kubectl get all -A
# Or press Ctrl+D
exit
```

Once you're done, throw it away
```bash
kink delete cluster
```

If you don't have a cluster to test with, you can use [KinD](https://github.com/kubernetes-sigs/kind) to create a cluster in a single docker container, and the nesting will work as you expect.

```bash
kind create cluster --kubeconfig=./kind.kubeconfig
kind load docker-image <image registry>/<image name>:<image tag>
kink --kubeconfig=./kind.kubeconfig ...
```

## Other Configurations

### RKE2

RKE2 is derrived from k3s, and is similar enough that you can switch to using it by adding `--set rke2.enabled=true` in your `kink create cluster` command. Do note, that due to changes in how the embedded storage is handled, RKE2 requires an HA controlplane. Note that switching between k3s and RKE2 on the same cluster is not supported, the new cluster will not contain any of your previous state.

### Single-node

If having two nodes is still two heavyweight, you can use a single-node setup by adding `--set worker.replicaCount=0,controlplane.defaultTaint=false`. For obvious reasons, this will not work with RKE2.

### ReadWriteMany Storage

If your parent cluster supports ReadWriteMany storage, you can leverage this in your KinK cluster as well by adding `--set sharedPersistence.enabled=true`. By default, KinD does not support this. You can use `hack/add-kind-shared-storage.sh` to add this support. If your KinD cluster has multiple nodes, you wil need an idential host mount on all KinD nodes.
