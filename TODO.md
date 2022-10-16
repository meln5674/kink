* Test persistence
* Test ingress
* Test running unprivileged in a rootless setup
* See how many times we can go deeper before something breaks
* Autoscaling for workers
* Make a CLI that manages clusters similar to kind, using helm, extracts kubeconfig, and handles port-forwarding to the controlplane and workers, adds ability to import images into all of the workers
* Make an operator that lets you request a cluster via a CRD
* Make a version that uses kindest/node?
* Add an optional post-hook job which pulls the kubeconfig into a secret via kubectl exec and kubectl create secret
* Language bindings for in-language tests?
