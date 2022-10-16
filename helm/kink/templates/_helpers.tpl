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

{{- define "kink.controlplane.url" -}}
https://{{ include "kink.controlplane.fullname" . }}:{{ .Values.controlplane.service.api.port }}
{{- end -}}
