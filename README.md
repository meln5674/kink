You've heard of [Kubernetes in Docker (KinD)](https://github.com/kubernetes-sigs/kind), now get ready for

# Kubernetes in Kubernetes (KinK)

## What?

Deploy a "guest" Kubernetes cluster inside of another "host" Kubernetes cluster, with all of the HA, scalability, and cluster features you would expect.

Kink is currently made up of two components:
* A Helm Chart which deploys a nested Cluster
* A CLI tool to automate using the above chart, as well as normal tasks like loading images, and providing access to clusters which aren't otherwise exposed

## Why?

Even if you have access to a Kubernetes cluster, odds are, you don't have cluster admin permissions to it, which means you cannot do things like install custom resource defintions (CRDs). This makes testing custom operators and other tools that manage Kubernetes itself a pain. When deploying an application in Kubernetes as part of a CI/CD pipeline integration test or staging environment, there may be extra state floating around that may interfere with or even invalidate the results of your tests. Using KinK, you can create a completely fresh cluster each time, just like using KinD, but in a Kubernetes cluster, leveraging as much scalability as the parent cluster affords, and usable from within another Pod.

While it is entirely possible to use a cloud provider like AWS, GCP, or Azure to create a new cluster as part of a CI/CD pipeline, this is both insecure and wasteful, as it requires providing your developers the ability to create (and more importantly, delete!) all of the resources required to do so, and requires you to pay for those resources, even if you aren't fully utilizing them. By creating a new nested cluster inside of your existing software factory cluster, you can place more restrictions on what permissions developers require, and leverage your existing factory autoscaling to maximize resource utilization and descrease costs.

## How?

KinK is based off of [k3s](https://k3s.io/), a super-lightweight Kubernetes distribution, which includes networking, storage, and ingress. KinK also supports using [RKE2](https://docs.rke2.io/), a more "enterprise-ready" version of k3s.

## Getting Started

### Prerequisites

Kink depends on the following tools being available on the $PATH

* [docker](https://docs.docker.com/engine/install/) (Only necessary if you intend to load images from a docker daemon into your cluster)
* [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl)
* [helm](https://helm.sh/docs/intro/install/)

### Installation

```bash
go install github.com/meln5674/kink@latest
```

### Create Cluster

You'll now be able to deploy your cluster

```bash
kink create cluster \
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
kink --kubeconfig=./kind.kubeconfig ... # Or export KUBECONFIG=./kind.kubeconfig
```

## Configuration File

The `kind create cluster` command has many flags, and setting them all can be inconvenient. Instead, you can collect all flags into a configuration file with the following schema

```yaml
apiVersion: kink.meln5674.github.com/v0
Kind: Config
docker:
  command: [] # --docker-command
kubernetes:
  kubeconfig: "" # --kubeconfig
kubectl:
  command: [] # --kubectl-command
helm:
  command: [] # --helm-command
chart:
  chart: "" # --chart
  repositoryURL: "" # --repository-url
release:
  clusterName: "" # --name
  values: [] # --values
  set: {} # --set
  setString: {} # --set-string
  
```

Specify the path to such a configuration file with the `--config` flag.

This file is specified via the type contained in [this file](./pkg/config/config.go) in the event you wish to produce or manipulation one programatically

## Common Tasks and Configurations

### RKE2

RKE2 is derrived from k3s, and is similar enough that you can switch to using it by adding `--set rke2.enabled=true` in your `kink create cluster` command. Do note, that due to changes in how the embedded storage is handled, RKE2 requires an HA controlplane. Note that switching between k3s and RKE2 on the same cluster is not supported, the new cluster will not contain any of your previous state.

### Single-node

If having two nodes is still two heavyweight, you can use a single-node setup by adding the following flags

```
--set worker.replicaCount=0
--set controlplane.defaultTaint=false
```

For the above reasons, this will not work with RKE2.

By default, `kink load` will only target worker nodes, so you will need to include `--only-load-workers=false` in each such command.

### ReadWriteMany Storage

If your parent cluster supports ReadWriteMany storage, you can leverage this in your KinK cluster as well by adding `--set sharedPersistence.enabled=true`. By default, KinD does not support this. You can use `hack/add-kind-shared-storage.sh` to add this support. If your KinD cluster has multiple nodes, you wil need an idential host mount on all KinD nodes.

### Legacy IPTables

If your kernel does not support nftables, then you will see errors such as ```Couldn't load match `comment':No such file or directory```, and your cluster will fail to start. You can `--set iptables.useLegacy=true` to resolve this.

### Controlplane Access

There are currently four supported ways of accessing your cluster's controlplane:

#### Port-Forwarding

This is the default method. When exporting your kubeconfig, this will be assumed if the other options are not selected. With this method, it is assumed that you are running a `kubectl port-forward` on your controlplane service with the same port on both local and remote. The `kink port-forward` command will perform this for you, and the `kink exec` and `kink sh` commands will do this in the background before executing your commands.

#### NodePort

If you `--set controlplane.service.type=NodePort`, your controlplane service will be given a NodePort. You must also then `--set controlplane.nodeportHost` to a hostname that will reliably forward all traffic to the matching NodePort on your host cluster. Exporting your kubeconfig with this set will create a new context called `external` with this URL set as the default.

#### Ingress

If you `--set controlplane.ingress.enabled`, as well as the [remaining configuration](helm/kink/values.yaml), then an Ingress resource will be created on the host cluster that will direct traffic to your controllplane. Note that this requires SSL passthrough, which not all ingress controllers support. Exporting your kubeconfig with this set will create a new context called `external` with this URL set as the default.

#### In-Cluster

If you wish to access the guest controlplane from within a pod in the host cluster, `--set kubeconfig.enabled=true`. This will run a job that will wait for the cluster to become ready, then export a kubeconfig set to use the *.svc.cluster.local hostname into a secret in the host cluster. You can then mount this secret into your host cluster pods.

### NodePorts and LoadBalancers

If you wish to wish to access NodePort and LoadBalancer type services within the host cluster, you can do so by directly accessing individual pods, or, if a load-balancer for your LoadBalancers is desirable, `--set loadBalancer.enabled=true` to enable an additional component which will dynamically manage a service that will contain all detected NodePorts (including LoadBalancers). Presently, the status.ingress field will report ingresses that are usable within the cluster.

### Nested Ingress Controllers

If you wish to utilize Ingress resources in your guest cluster through your host cluster's ingress controller, `--set loadBalancer.enabled=true --set loadBalancer.ingress.enabled=true`. This will dynamically create and manage a set of host Ingress resources based on "Class Mappings", which indicate how to get traffic to your guest cluster ingress controller for a given guest cluster ingressClassName. Any number of host and guest ingress controllers and classes are supported. Currently, only ingress controllers which expose themselves as container hostPorts or as NodePort/LoadBalancer services as supported. See [here](helm/kink/values.yaml) for the syntax on defining these mappings. Note that you must pick between your guest ingress controller's HTTP or HTTPS port, you cannot choose both due to how ingresses work. If you choose HTTP, then your host cluster will terminate TLS, which some services may not tolerate. If you choose HTTPS, then your ingress must be set to use SSL passthrough, which your host ingress controller may not support.

### Other Ingress

If you wish to use host cluster Ingresses for traffic other than a guest cluster ingress controller, `--set loadBalancer.ingress.enabled=true` like with a nested ingress controller. Then, instead of defining `classMapping`s, instead use the [static](helm/kink/values.yaml) section. These static ingresses can likewise target a NodePort/LoadBalancer service in the guest cluster, or a container hostPort. This can be used, for example, to route traffic to an Istio Gateway. The same caveats regarding HTTP/HTTPS ports apply as with nested ingress controllers.

### Air-gapped Clusters

For initial setup, see [here for k3s](https://docs.k3s.io/installation/airgap#prepare-the-images-directory-and-k3s-binary) and [here for rke2](https://docs.rke2.io/install/airgap/#tarball-method). You can then make these files and directories available to your cluster pods in a ReadWriteMany PVC using `--set extraVolumes` and `--set extraVolumeMounts`. Once you cluster is started, you can load additional images using `kink load docker-image <image name on local daemon>`, `kink load docker-archive <path to tarball>` and `kink load oci-archive <path to tarball>`. For accessing the chart, use the `--chart` flag to `kink create cluster` to specify a path to a local checkout of the chart or chart tarball, or use the `--repository-url` flag to specify an accessible chart repository in which you've mirrored the chart.

### Private Registries

The `registries` field within the `values.yaml` will be saved as the `registries.yaml` file for k3s/rke2. To load credentials and TLS files from a secret or configmap, set the `auth.volume` and/or `tls.volume` fields (which will be stripped from the final file) to the appropriate volume to add to the pod spec, and instead set the username, password, ca_file, etc, fields to the subPath within those volumes.

### HTTP Proxies

You can set arbitrary extra environment variables with the `extraEnv` section. For k3s and rke2, you wil likely need to set `CONTAINERD_HTTP_PROXY`, `CONTAINERD_HTTPS_PROXY` and `CONTAINERD_NO_PROXY` to be able to pull images through a proxy. Setting `HTTP_PROXY` and the like will likely break the internal cluster networking, so only set it if you're absolutely sure you know what you're doing.

### Extra Charts

You can include extra `helm.cattle.io.HelmChart` resource manifests in the directories `/etc/kink/extra-manifests/{k3s,rke2}/user/` (depending on which distribution you are using), either through `--set controlplane.extraVolumes` and `--set controplane.extraVolumeMounts` or by building a derrived image, which will be copied on controlplane startup and will result in the charts being deployed.

### In-Cluster/External Loadbalancer Use

The kubeconfig file generated by `kink create cluster` and `kink export kubeconfig` will include multiple contexts. `default` assumes you are using `kink exec`, `kink sh`, or `kink port-forward`, and will use localhost. `in-cluster` assumes you are running in a pod in the same cluster as the nested cluster, and will use coredns-resolvable hostnames. If you know ahead of time that your controlplane will be accessible at an external address, such as through an ingress controller or LoadBalancer service, you can provide that URL with the `--external-controlplane-url` to `kink create cluster` or `kink export kubeconfig` to also include an `external` context which will use that URL.

### Least Privilege

See [here](./examples/role.yaml) for a role with the minimal set of permissions needed to use kind.

### Test Local Chart and Images

If you are making a fork and wish to test your local version, use `--set image.repository`, `--set image.tag` to point to your locally built image (or within your private image registry, along with `--set imagePullSecrets[0].name`, if necessary), and use `--chart` to point to a local chart, or use `--repository-url`, `--chart`, and `--chart-version` to point to a private chart repository.

### Off-$PATH Commands, Debug Logs, and Extra Flags

KinK uses [klog](https://github.com/kubernetes/klog), so it uses the same flags as common tools like kubectl, e.g. `-v` to set logging options. To enable debugging logs for the tools KinK calls out to, use the `--*-command` flags, e.g. `--kubectl-command=kubectl,-v,10` or `--helm-command=helm,--debug`. This can also be used to specify an absolute path or non-default name for these tools, as well as to add arbitrary extra flags to them.
