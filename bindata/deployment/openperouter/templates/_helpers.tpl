{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "openperouter.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "openperouter.fullname" -}}
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

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "openperouter.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "openperouter.labels" -}}
helm.sh/chart: {{ include "openperouter.chart" . }}
{{ include "openperouter.selectorLabels" . }}
{{- if .Chart.AppVersion }}
version: {{ .Chart.AppVersion | quote }}
{{- end }}
managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "openperouter.selectorLabels" -}}
name: {{ include "openperouter.name" . }}
instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the openperouter service accounts to use
*/}}
{{- define "openperouter.controller.serviceAccountName" -}}
{{- if .Values.openperouter.serviceAccounts.create }}
{{- default (printf "%s-controller" (include "openperouter.fullname" .)) .Values.openperouter.serviceAccounts.controller.name }}
{{- else }}
{{- default "default-controller" .Values.openperouter.serviceAccounts.controller.name }}
{{- end }}
{{- end }}

{{- define "openperouter.router.serviceAccountName" -}}
{{- if .Values.openperouter.serviceAccounts.create }}
{{- default (printf "%s-perouter" (include "openperouter.fullname" .)) .Values.openperouter.serviceAccounts.perouter.name }}
{{- else }}
{{- default "default-perouter" .Values.openperouter.serviceAccounts.perouter.name }}
{{- end }}
{{- end }}
