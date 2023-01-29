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
* Add an optional post-hook job which pulls the kubeconfig into a secret via kubectl exec and kubectl create secret
* Language bindings for in-language tests?
* Forward signals in exec/sh
* Find way to forward cluster helm values to bundled chart (e.g local-path-provisioner) values
* Find funding and write integration tests which leverage CSP's
* Set up mage to build multiple exe's
* Set up actions to publish exe's, chart, and image
    * Run integration tests in actions and see how long until I get rate limited
* Create secret w/ kubeconfig on cluster creation. Add separate clusters/contexts for localhost, in-cluster, and via external access via url provided by flag
* Extract command functions into pkg/ for use in other projects
* Implement LoadBalancer services:
    * Each load balancer would create a service in the host cluster that selects all worker pods, the load balancer IP would then be the service cluster IP
    * To implement initally, add a template to generate said services to helm chart, 'create cluster' would then scan for loadbalancer-type services in the guest cluster and automatically add --set flags
* Use a similar approach to loadbalancer to map guest ingress hosts and paths to host hosts and paths
* Eventually, write a service which is installed as a deployment which monitors the guest cluster for ingresses and load balancers and perfoms the above updates automatically
* Switch commands that need controlplane access from using port-forward to just exec'ing on an available controlplane node
* After chart is upgraded, wait for all nodes to become ready
* Refactor ginkgo integration tests into a command that can be used to stand up a dev env like the shell versions allow
