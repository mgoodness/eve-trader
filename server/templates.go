package server

import (
	"embed"
	"html/template"
)

//go:embed templates/*.html
var templateFS embed.FS

// tmpl parses all templates once at startup: "page.html" for the full
// index route, "rows" (a named define, reused across templates/page.html
// and templates/rows.html) for the htmx sort-partial route, and
// "footnote" for the persistent Heimatar-region-volume caveat.
var tmpl = template.Must(template.New("").Funcs(templateFuncs).ParseFS(templateFS, "templates/*.html"))
