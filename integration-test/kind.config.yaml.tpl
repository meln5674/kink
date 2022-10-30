kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraMounts:
  - hostPath: ${PWD}/integration-test/shared-local-path-provisioner
    containerPath: /var/shared-local-path-provisioner
  - hostPath: ${PWD}/integration-test/local-path-provisioner
    containerPath: /var/local-path-provisioner
  - hostPath: ${PWD}
    containerPath: /src/kink
  - hostPath: /var/run/docker.sock
    containerPath: /var/run/docker.sock
  extraPortMappings:
  - containerPort: 80
    hostPort: 80
    listenAddress: "127.0.0.1"
    protocol: TCP 
  - containerPort: 443
    hostPort: 443
    listenAddress: "127.0.0.1"
    protocol: TCP 
