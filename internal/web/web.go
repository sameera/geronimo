// Package web holds the HTML templates and static assets.
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

var templates = template.Must(template.ParseFS(templateFS, "templates/*.html"))

// Render writes the named page. Pages are signed-in or signed-out; both share one layout.
func Render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = templates.ExecuteTemplate(w, name, data)
}

// Static serves /static/ assets.
func Static() http.Handler {
	sub, _ := fs.Sub(staticFS, "static")
	return http.StripPrefix("/static/", http.FileServerFS(sub))
}
