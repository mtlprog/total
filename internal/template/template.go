package template

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"net/url"
	"strings"
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
	"urlencode": url.QueryEscape,
	"labURL": func(xdr, networkPassphrase string) string {
		return "https://lab.stellar.org/transaction/cli-sign?" +
			"networkPassphrase=" + url.QueryEscape(networkPassphrase) +
			"&xdr=" + url.QueryEscape(xdr)
	},
	"truncate": func(s string, n int) string {
		if len(s) <= n {
			return s
		}
		return s[:n] + "..."
	},
	"shortID": func(s string) string {
		if len(s) <= 19 {
			return s
		}
		return s[:8] + "..." + s[len(s)-8:]
	},
	"isTestnet": func(passphrase string) bool {
		return strings.Contains(passphrase, "Test")
	},
	"networkName": func(passphrase string) string {
		if strings.Contains(passphrase, "Test") {
			return "testnet"
		}
		return "public"
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
