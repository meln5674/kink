cat <<EOF
apiVersion: helm.cattle.io/v1
kind: HelmChart
metadata:
  annotations:
    helm.cattle.io/chart-url: https://raw.githubusercontent.com/meln5674/kink/${KINK_REF}/${CHART_PATH}
  creationTimestamp: null
  name: kink-local-path-provisioner
  namespace: kube-system
spec:
  bootstrap: true
  set:
    storageClass.defaultClass: "true"
  chartContent: |
$(cat "${CHART_PATH}" | base64 | sed -E 's/^/    /g')
EOF
