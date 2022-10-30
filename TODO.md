* Finish RKE2 support
* Test ingress
* Add option for shared storage
    * This would be done by adding a second local-path-provisioner pointed at /opt/shared-local-path-provisioner, and a standalone ReadWriteMany PVC, and mounting it there for all nodes
* Add option for multiple storage classes
    * In theory, it should be possible to offer multiple storage classes by creating multiple instances of the local path provisioner at multiple mount points backed by different real PVCs of different storage classes, and creating StorageClasses with identical names. This should, again in theory, make it possible to test applications which depend on different host storage classes
    * Would need to find a suitable test app
    * Having separate SC's for this would be a pain, maybe it'd be worth it to fork local-path-provisioner?
* Test running unprivileged in a rootless setup
* See how many times we can go deeper before something breaks
* Autoscaling for workers
* PodDisruptionPolicy for HA controlplane
* PodDisruptionPolicy for workers for, e.g. maintaining availability for apps
* Make an operator that lets you request a cluster via a CRD
    * Would create a cluster named as the UID of the cluster CR using in-cluster permissions
    * Would helm upgrade into its own namespace, and add kubeconfig to cluster CR status
        * This seems insecure, but is in fact the most secure option, as it means that requesting a cluster requires /only/ access to the cluster CR within a given namespace, and not even access to secrets. Not deploying to the same namespace as the CR means that tenants cannot exec into the priviledged containers, and can only access over the k8s API. This allows for providing permissions to request a cluster as part of a single-namespace pipeline (e.g. Jenkins) without the risk of accessing secrets in the same namespace.
* Make a version that uses kindest/node? - Probably not
* Add an optional post-hook job which pulls the kubeconfig into a secret via kubectl exec and kubectl create secret
* Language bindings for in-language tests?
* Add integration tests for running in-cluster
* Forward signals in exec/sh
* Find way to forward cluster helm values to bundled chart (e.g local-path-provisioner) values
* Find funding and write integration tests which leverage CSP's
* Set up mage to build multiple exe's
* Set up actions to publish exe's, chart, and image
    * Run integration tests in actions and see how long until I get rate limited
* Re-write integration tests in Go using Ginkgo
* Create secret w/ kubeconfig on cluster creation. Add separate clusters/contexts for localhost, in-cluster, and via external access via url provided by flag
