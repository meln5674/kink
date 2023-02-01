* Test ingress
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
* Language bindings for in-language tests?
* Forward signals in exec/sh
* Find way to forward cluster helm values to bundled chart (e.g local-path-provisioner) values
    * https://docs.k3s.io/helm#customizing-packaged-components-with-helmchartconfig
    * https://docs.rke2.io/helm?_highlight=helmchartconfig#customizing-packaged-components-with-helmchartconfig
    * Should be possible to take these extra values/versions/repos from kink values.yaml and update/add them via yq in init container
* Find funding and write integration tests which leverage CSP's
* Set up mage to build multiple exe's
* Set up actions to publish exe's, chart, and image
    * Run integration tests in actions and see how long until I get rate limited
* Extract command functions into pkg/ for use in other projects
* Use a similar approach to loadbalancer to map guest ingress hosts and paths to host hosts and paths
* Switch commands that need controlplane access from using port-forward to just exec'ing on an available controlplane node
* After chart is upgraded, wait for all nodes to become ready
* Refactor ginkgo integration tests into a command that can be used to stand up a dev env like the shell versions allow
* Add commands to generate a stub chart (no templates, dependency on kink chart, with values), argocd Applications, and fluxcd HelmReleases from a given config file for use in gitops
* Forward logs from all pods to a central log aggregation pod so that pod logs can be shipped to host cluster logging
* Switch from using binaries for kubectl, helm, kind, etc, to importing them as libraries
