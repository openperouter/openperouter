{{- $hasFeature := false -}}
{{- $hasBug := false -}}
{{- $hasCleanup := false -}}
{{- $hasDemo := false -}}
{{- range .Notes -}}
  {{- if eq .Kind "feature" -}}{{- $hasFeature = true -}}{{- end -}}
  {{- if eq .Kind "bug" -}}{{- $hasBug = true -}}{{- end -}}
  {{- if eq .Kind "demo" -}}{{- $hasDemo = true -}}{{- end -}}
  {{- if eq .Kind "Other (Cleanup or Flake)" -}}{{- $hasCleanup = true -}}{{- end -}}
{{- end }}
{{- if $hasFeature }}

### New Features

{{- range .Notes }}
{{- if eq .Kind "feature" }}
{{- range .NoteEntries }}
- {{.}}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
{{- if $hasBug }}

### Bug fixes

{{- range .Notes }}
{{- if eq .Kind "bug" }}
{{- range .NoteEntries }}
- {{.}}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
{{- if $hasDemo }}

### Examples

{{- range .Notes }}
{{- if eq .Kind "example" }}
{{- range .NoteEntries }}
- {{.}}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
{{- if $hasCleanup }}

### Other (Cleanup or Flake)

{{- range .Notes }}
{{- if eq .Kind "Other (Cleanup or Flake)" }}
{{- range .NoteEntries }}
- {{.}}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
