package lazyexp

import (
	"io"
	"strings"
	"text/template"
)

func dotEscapeLabel(label string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		"\n", `\n`,
		`"`, `\"`,
	).Replace(label)
}

func isChild(id ID) bool {
	return id != NoChild
}

func discarded(err error) bool {
	return err == Discarded
}

var dotTemplate = template.Must(template.New("dot").Funcs(
	template.FuncMap{
		"escapeLabel": dotEscapeLabel,
		"isChild":     isChild,
		"discarded":   discarded,
	}).Parse(`digraph {
{{- range $id, $node := .}}
	node{{$id}} [label="{{$node.Description | escapeLabel}}"
		{{- if $node.Evaluated -}}
			,color={{if $node.Err}}red{{else}}green{{end}}
		{{- end -}}
	];
	{{- range $dep := $node.Dependencies}}
	node{{$dep.ID}} -> node{{$id}} [
		{{- if discarded $dep.Err -}}
			color=grey
		{{- else if $dep.Err -}}
			color=red
		{{- else -}}
			color=green
		{{- end -}}
	];
	{{- end}}
	{{- if isChild $node.Child}}
	node{{$node.Child}} -> node{{$id}} [style=dotted];
	{{- end}}
{{- end}}
}
`))

// Dot serializes the given FlattenedNodes to the GraphViz Dot format.
func Dot(wr io.Writer, fn FlattenedNodes) error {
	return dotTemplate.Execute(wr, fn)
}
