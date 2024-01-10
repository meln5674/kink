{{- $msg := "This is the file output by the template, DONT EDIT IT" }}
# {{ $msg }}
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraMounts:
  # To test PVCs, we need at least two host mounts for the local provisioner
  - hostPath: {{ .Pwd }}/../integration-test/volumes/var/shared-local-path-provisioner
    containerPath: /var/shared-local-path-provisioner
  # These are needed in order to run in-cluster tests 
  - hostPath: {{ .Pwd }}/..
    containerPath: /src/kink/kink
  - hostPath: {{ .Pwd }}/../..
    containerPath: /src/kink/
  - hostPath: /var/run/docker.sock
    containerPath: /var/run/docker.sock
  # Not necessary, but this should be faster than using the overlayfs
  - hostPath: {{ .Pwd }}/../integration-test/volumes/var/local-path-provisioner
    containerPath: /var/local-path-provisioner
  - hostPath: {{ .Pwd }}/../integration-test/volumes/var/lib/kubelet/
    containerPath: /var/lib/kubelet
  - hostPath: {{ .Pwd }}/../integration-test/volumes/var/log/
    containerPath: /var/log/
  - hostPath: {{ .Pwd }}/../integration-test/volumes/var/lib/containerd/io.containerd.runtime.v1.linux
    containerPath: /var/lib/containerd/io.containerd.runtime.v1.linux
  - hostPath: {{ .Pwd }}/../integration-test/volumes/var/lib/containerd/io.containerd.snapshotter.v1.btrfs
    containerPath: /var/lib/containerd/io.containerd.snapshotter.v1.btrfs
  - hostPath: {{ .Pwd }}/../integration-test/volumes/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs/snapshots
    containerPath: /var/lib/containerd/io.containerd.snapshotter.v1.overlayfs/snapshots
  - hostPath: {{ .Pwd }}/../integration-test/volumes/var/lib/containerd/io.containerd.snapshotter.v1.native/snapshots
    containerPath: /var/lib/containerd/io.containerd.snapshotter.v1.native/snapshots
  - hostPath: {{ .Pwd }}/../integration-test/volumes/var/lib/containerd/io.containerd.runtime.v2.task
    containerPath: /var/lib/containerd/io.containerd.runtime.v2.task
  - hostPath: {{ .Pwd }}/../integration-test/volumes/var/lib/registry
    containerPath: /var/lib/registry
