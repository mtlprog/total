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

// Template functions available in all templates.
var funcMap = template.FuncMap{
	"mul": func(a, b float64) float64 {
		return a * b
	},
	"div": func(a, b float64) float64 {
		if b == 0 {
			return 0
		}
		return a / b
	},
	"add": func(a, b float64) float64 {
		return a + b
	},
	"sub": func(a, b float64) float64 {
		return a - b
	},
}

func New() (*Template, error) {
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templates, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}
	return &Template{tmpl: tmpl}, nil
}

func (t *Template) Render(w io.Writer, name string, data any) error {
	return t.tmpl.ExecuteTemplate(w, name+".html", data)
}
