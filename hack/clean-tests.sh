#!/bin/bash -xue

docker exec -i kink-it-control-plane bash <<'EOF'
set -xeu
systemctl stop kubelet
crictl ps | tail -n +2 | awk '{ print $1 }' | xargs crictl stop
crictl ps -a | tail -n +2 | awk '{ print $1 }' | xargs crictl rm
ctr -n k8s.io task ls | tail -n +2 | while read -r task rest ; do ctr -n k8s.io task kill ${task} ; done
ctr -n k8s.io container ls | tail -n +2 | while read -r container rest ; do ctr -n k8s.io container rm ${container} ; done
systemctl stop containerd
mount | grep /run/containerd/io.containerd.grpc.v1.cri/sandboxes/ | while read -r type on path rest; do umount $path ; done
mount | grep /var/lib/kubelet/pods/ | grep '/volumes/' | while read -r type on path rest; do umount $path ; done
rm -rf /var/lib/kubelet/* /var/lib/kubelet/containerd/*/* /var/log/* /var/{shared-,}local-path-provisioner/*
EOF
