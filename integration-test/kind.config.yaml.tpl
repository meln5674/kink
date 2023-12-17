kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraMounts:
  # To test PVCs, we need at least two host mounts for the local provisioner
  - hostPath: ${PWD}/integration-test/volumes/var/shared-local-path-provisioner
    containerPath: /var/shared-local-path-provisioner
  # These are needed in order to run in-cluster tests 
  - hostPath: ${PWD}
    containerPath: /src/kink
  - hostPath: /var/run/docker.sock
    containerPath: /var/run/docker.sock
  # Not necessary, but this should be faster than using the overlayfs
  - hostPath: ${PWD}/integration-test/volumes/var/local-path-provisioner
    containerPath: /var/local-path-provisioner
  - hostPath: ${PWD}/integration-test/volumes/var/lib/kubelet/
    containerPath: /var/lib/kubelet
  - hostPath: ${PWD}/integration-test/volumes/var/log/
    containerPath: /var/log/
  - hostPath: ${PWD}/integration-test/volumes/var/lib/containerd/io.containerd.runtime.v1.linux
    containerPath: /var/lib/containerd/io.containerd.runtime.v1.linux
  - hostPath: ${PWD}/integration-test/volumes/var/lib/containerd/io.containerd.snapshotter.v1.btrfs
    containerPath: /var/lib/containerd/io.containerd.snapshotter.v1.btrfs
  - hostPath: ${PWD}/integration-test/volumes/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs/snapshots
    containerPath: /var/lib/containerd/io.containerd.snapshotter.v1.overlayfs/snapshots
  - hostPath: ${PWD}/integration-test/volumes/var/lib/containerd/io.containerd.snapshotter.v1.native/snapshots
    containerPath: /var/lib/containerd/io.containerd.snapshotter.v1.native/snapshots
  - hostPath: ${PWD}/integration-test/volumes/var/lib/containerd/io.containerd.runtime.v2.task
    containerPath: /var/lib/containerd/io.containerd.runtime.v2.task
  
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
  - containerPort: 30005
    hostPort: 30005
    listenAddress: "127.0.0.1"
    protocol: TCP
  - containerPort: 30006
    hostPort: 30006
    listenAddress: "127.0.0.1"
    protocol: TCP
  - containerPort: 30007
    hostPort: 30007
    listenAddress: "127.0.0.1"
    protocol: TCP
  - containerPort: 30008
    hostPort: 30008
    listenAddress: "127.0.0.1"
    protocol: TCP
  - containerPort: 30009
    hostPort: 30009
    listenAddress: "127.0.0.1"
    protocol: TCP
  kubeadmConfigPatches:
  - |
    kind: ClusterConfiguration
    apiServer:
      extraArgs:
        # To test nodeport controlplane access, we need to expose whatever nodeport gets picked for it
        # In order to not have to export every possible nodeport, we make it so that there are only as
        # many ports as the tests need, and only expose those
        service-node-port-range: 30000-30007

