kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraMounts:
  # To test PVCs, we need at least two host mounts for the local provisioner
  - hostPath: ${PWD}/integration-test/shared-local-path-provisioner
    containerPath: /var/shared-local-path-provisioner
  - hostPath: ${PWD}/integration-test/local-path-provisioner
    containerPath: /var/local-path-provisioner
  # These are needed in order to run in-cluster tests 
  - hostPath: ${PWD}
    containerPath: /src/kink
  - hostPath: /var/run/docker.sock
    containerPath: /var/run/docker.sock
  extraPortMappings:
  # To test controlplane ingress 
  - containerPort: 80
    hostPort: 80
    listenAddress: "127.0.0.1"
    protocol: TCP 
  - containerPort: 443
    hostPort: 443
    listenAddress: "127.0.0.1"
    protocol: TCP 
  # See below
  - containerPort: 30000
    hostPort: 30000
    listenAddress: "127.0.0.1"
    protocol: TCP
  - containerPort: 30001
    hostPort: 30001
    listenAddress: "127.0.0.1"
    protocol: TCP
  - containerPort: 30002
    hostPort: 30002
    listenAddress: "127.0.0.1"
    protocol: TCP
  - containerPort: 30003
    hostPort: 30003
    listenAddress: "127.0.0.1"
    protocol: TCP
  - containerPort: 30004
    hostPort: 30004
    listenAddress: "127.0.0.1"
    protocol: TCP
  kubeadmConfigPatches:
  - |
    kind: ClusterConfiguration
    apiServer:
      extraArgs:
        # To test nodeport controlplane access, we need to expose whatever nodeport gets picked for it
        # In order to not have to export every possible nodeport, we make it so that there are only as
        # many ports as the controlplane service needs, and only expose those
        service-node-port-range: 30000-30005

