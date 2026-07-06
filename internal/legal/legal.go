// Package legal serves third-party license notices over HTTP.
//
// The notices satisfy the attribution requirements of the permissive licenses
// (MIT, BSD, Apache-2.0) used by OpenLore's bundled dependencies: when a binary
// or image that includes those dependencies is distributed, recipients can read
// the notices and full license texts from the running service at /legal.
package legal

import (
	"html"
	"io/fs"
	"net/http"
	"sort"
	"strings"
)

const noticesFile = "THIRD_PARTY_NOTICES.md"

// Handler returns an http.Handler that serves third-party legal notices.
//
// fsys is the embedded legal filesystem (assets.Legal()), rooted at the legal/
// directory: it contains THIRD_PARTY_NOTICES.md and a licenses/ subdirectory.
//
// Routes (mount the handler on the /legal subtree):
//
//	GET /legal                        HTML notices page
//	GET /legal/THIRD_PARTY_NOTICES.md raw Markdown
//	GET /legal/licenses/<name>        raw license text
func Handler(fsys fs.FS) http.Handler {
	mux := http.NewServeMux()

	fileServer := http.FileServer(http.FS(fsys))
	// Raw license files: /legal/licenses/<name> -> licenses/<name> in fsys.
	mux.Handle("/legal/licenses/", http.StripPrefix("/legal/", fileServer))

	// Raw notices Markdown.
	mux.HandleFunc("/legal/"+noticesFile, func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(fsys, noticesFile)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Write(data)
	})

	index := indexHandler(fsys)
	mux.HandleFunc("/legal", index)
	mux.HandleFunc("/legal/", index)

	return mux
}

// indexHandler renders the human-readable notices page.
func indexHandler(fsys fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notices, err := fs.ReadFile(fsys, noticesFile)
		if err != nil {
			http.Error(w, "legal notices not available", http.StatusInternalServerError)
			return
		}

		licenses := listLicenses(fsys)

		var b strings.Builder
		b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Third-Party Notices — OpenLore</title>
<style>
body{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;max-width:52rem;margin:2rem auto;padding:0 1rem;line-height:1.5;color:#1a1a1a}
pre{white-space:pre-wrap;word-wrap:break-word;background:#f6f6f4;padding:1rem;border-radius:6px;overflow-x:auto}
h1{font-size:1.4rem}
ul{padding-left:1.2rem}
a{color:#4a5d23}
</style>
</head>
<body>
<h1>Third-Party Notices</h1>
<p>Full license texts: <a href="` + noticesFile + `">raw notices</a></p>
<pre>`)
		b.WriteString(html.EscapeString(string(notices)))
		b.WriteString("</pre>\n")

		if len(licenses) > 0 {
			b.WriteString("<h2>License files</h2>\n<ul>\n")
			for _, name := range licenses {
				esc := html.EscapeString(name)
				b.WriteString(`<li><a href="licenses/` + esc + `">` + esc + "</a></li>\n")
			}
			b.WriteString("</ul>\n")
		}

		b.WriteString("</body>\n</html>\n")

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(b.String()))
	}
}

// listLicenses returns the sorted names of files in the licenses/ directory.
func listLicenses(fsys fs.FS) []string {
	entries, err := fs.ReadDir(fsys, "licenses")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}
