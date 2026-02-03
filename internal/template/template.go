package template

import (
	"embed"
	"fmt"
	"html/template"
	"io"
)

//go:embed templates/*.html
var templates embed.FS

type Template struct {
	tmpl *template.Template
}

func New() (*Template, error) {
	tmpl, err := template.ParseFS(templates, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}
	return &Template{tmpl: tmpl}, nil
}

func (t *Template) Render(w io.Writer, name string, data any) error {
	return t.tmpl.ExecuteTemplate(w, name+".html", data)
}
