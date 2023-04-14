{{/*
Expand the name of the chart.
*/}}
{{- define "kink.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "kink.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "kink.controlplane.fullname" -}}
{{- include "kink.fullname" . }}-controlplane
{{- end }}

{{- define "kink.worker.fullname" -}}
{{- include "kink.fullname" . }}-worker
{{- end }}

{{- define "kink.lb-manager.fullname" -}}
{{- include "kink.fullname" . }}-lb-manager
{{- end }}

{{- define "kink.load-balancer.fullname" -}}
{{- include "kink.fullname" . }}-lb
{{- end }}


{{- define "kink.kubeconfig.fullname" -}}
{{- include "kink.fullname" . }}-kubeconfig
{{- end }}


{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "kink.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "kink.labels" -}}
helm.sh/chart: {{ include "kink.chart" . }}
{{ include "kink.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "kink.controlplane.labels" -}}
{{ include "kink.labels" . }}
app.kubernetes.io/component: controlplane
{{- with .Values.controlplane.extraLabels }}
{{ . | toYaml }}
{{- end }}
{{- end -}}

{{- define "kink.worker.labels" -}}
{{ include "kink.labels" . }}
app.kubernetes.io/component: worker
{{- with .Values.worker.extraLabels }}
{{ . | toYaml }}
{{- end }}
{{- end -}}

{{- define "kink.lb-manager.labels" -}}
{{ include "kink.labels" . }}
app.kubernetes.io/component: lb-manager
{{- with .Values.loadBalancer.extraLabels }}
{{ . | toYaml }}
{{- end }}
{{- end -}}

{{- define "kink.load-balancer.labels" -}}
{{ include "kink.labels" . }}
app.kubernetes.io/component: load-balancer
{{- with .Values.loadBalancer.service.labels }}
{{ . | toYaml }}
{{- end }}
{{- end -}}

{{- define "kink.kubeconfig.labels" -}}
{{ include "kink.labels" . }}
app.kubernetes.io/component: kubeconfig
{{- with .Values.kubeconfig.extraLabels }}
{{ . | toYaml }}
{{- end }}
{{- end -}}



{{/*
Selector labels
*/}}
{{- define "kink.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kink.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "kink.controlplane.selectorLabels" -}}
{{ include "kink.selectorLabels" . }}
app.kubernetes.io/component: controlplane
{{- with .Values.controlplane.extraLabels }}
{{ . | toYaml }}
{{- end }}
{{- end -}}

{{- define "kink.worker.selectorLabels" -}}
{{ include "kink.selectorLabels" . }}
app.kubernetes.io/component: worker
{{- with .Values.worker.extraLabels }}
{{ . | toYaml }}
{{- end }}
{{- end -}}

{{- define "kink.lb-manager.selectorLabels" -}}
{{ include "kink.selectorLabels" . }}
app.kubernetes.io/component: lb-manager
{{- with .Values.loadBalancer.extraLabels }}
{{ . | toYaml }}
{{- end }}
{{- end -}}


{{/*
Create the name of the service account to use
*/}}
{{- define "kink.controlplane.serviceAccountName" -}}
{{- if .Values.controlplane.serviceAccount.create }}
{{- default (include "kink.controlplane.fullname" .) .Values.controlplane.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.controlplane.serviceAccount.name }}
{{- end }}
{{- end }}


{{- define "kink.worker.serviceAccountName" -}}
{{- if .Values.worker.serviceAccount.create }}
{{- default (include "kink.worker.fullname" .) .Values.worker.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.worker.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "kink.lb-manager.serviceAccountName" -}}
{{- if .Values.loadBalancer.manager.serviceAccount.create }}
{{- default (include "kink.lb-manager.fullname" .) .Values.loadBalancer.manager.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.loadBalancer.manager.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "kink.kubeconfig.serviceAccountName" -}}
{{- if .Values.kubeconfig.job.serviceAccount.create }}
{{- default (include "kink.kubeconfig.fullname" .) .Values.kubeconfig.job.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.kubeconfig.job.serviceAccount.name }}
{{- end }}
{{- end }}



{{- define "kink.controlplane.url" -}}
{{- if .Values.rke2.enabled -}}
https://{{ include "kink.controlplane.fullname" . }}:{{ (index .Values.controlplane.service "rke2-discover").port }}
{{- else -}}
https://{{ include "kink.controlplane.fullname" . }}:{{ .Values.controlplane.service.api.port }}
{{- end -}}
{{- end -}}

{{- define "kink.nodePortName" -}}
{{- $toSum := "" -}}
{{- if eq (kindOf .port) "string" -}}
{{- $toSum = printf "%s/%s/%s" .namespace .name .port -}}
{{- else if has (kindOf .port) (list "int64" "int32" "int" "float64" "float32") -}}
{{/* Helm renders any numeric values in a values.yaml as a float64, so we have to support them */}}
{{- $toSum = printf "%s/%s/%d" .namespace .name (int .port) -}}
{{- else -}}
{{- printf "nodePort ingress targets must set port name string or port number, but got %s" (kindOf .port) | fail -}}
{{- end -}}
{{- printf "np-0x%08x" (atoi (adler32sum $toSum)) }} # {{ $toSum }}, {{ adler32sum $toSum }}, {{ atoi (adler32sum $toSum) -}}
{{- end -}}

{{- define "kink.load-balancer.ingressHostPorts" -}}
{{- range .Values.loadBalancer.ingress.classMappings }}
{{- with .hostPort }}
{{- with .httpPort }}
- name: '{{ . }}'
  port: {{ . }}
  targetPort: {{ . }}
{{- end }}
{{- with .httpsPort }}
- name: '{{ . }}'
  port: {{ . }}
  targetPort: {{ . }}
{{- end }}
  protocol: TCP
{{- end }}
{{- end }}
{{- range .Values.loadBalancer.ingress.static }}
{{- with .hostPort }}
- name: '{{ . }}'
  port: {{ . }}
  targetPort: {{ . }}
  protocol: TCP
{{- end }}
{{- end }}
{{- end -}}
