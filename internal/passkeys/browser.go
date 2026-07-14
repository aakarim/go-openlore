package passkeys

import (
	"bytes"
	"fmt"
	"html"
	"mime"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/aakarim/go-openlore/pkg/vfs"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// LoreBrowserHandler serves an authenticated web browser over the filesystem.
// Unauthenticated requests are redirected to the passkey login page.
//
// fsForIdentity returns the per-identity, read-scoped filesystem for a resolved
// session identity — the SAME layered session FS used by the SSH shell, SFTP,
// and MCP/HTTP transports. The browser performs all Stat/ReadDir/ReadFile calls
// through that scoped FS, so docset boundaries (including the carve-out of nested
// docsets from an ancestor grant) are enforced identically here. There is no
// separate allow-list in the browser: the scoped FS is the sole authority on
// what a session may see.
func (p *Passkeys) LoreBrowserHandler(fsForIdentity func(identity string) vfs.FileSystem) http.Handler {
	lorePath := "/" + strings.Trim(p.cfg.LorePath, "/")
	if lorePath == "/" {
		lorePath = "/lore"
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := p.sessions.ValidateRequest(r)
		if !ok {
			redirect := url.QueryEscape(r.URL.Path)
			http.Redirect(w, r, "/passkey/login?redirect="+redirect, http.StatusFound)
			return
		}

		fsys := fsForIdentity(session.Identity)
		if fsys == nil {
			http.Error(w, "403 forbidden", http.StatusForbidden)
			return
		}

		// Map the request path under lorePath onto a filesystem path.
		rel := strings.TrimPrefix(r.URL.Path, lorePath)
		if rel == "" {
			http.Redirect(w, r, lorePath+"/", http.StatusFound)
			return
		}
		fsPath := vfs.CleanPath(rel)

		info, err := fsys.Stat(fsPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		if info.Dir {
			p.renderDir(w, fsys, lorePath, fsPath)
			return
		}

		data, err := fsys.ReadFile(fsPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("raw") != "1" {
			p.renderFile(w, r, lorePath, fsPath)
			return
		}
		if isMarkdown(fsPath) {
			if err := renderMarkdown(w, data); err != nil {
				http.Error(w, "failed to render markdown", http.StatusInternalServerError)
			}
			return
		}
		ctype := mime.TypeByExtension(path.Ext(fsPath))
		if ctype == "" {
			ctype = "text/plain; charset=utf-8"
		}
		w.Header().Set("Content-Type", ctype)
		w.Write(data)
	})
}

func (p *Passkeys) renderFile(w http.ResponseWriter, r *http.Request, lorePath, fsPath string) {
	parentURL := lorePath + path.Dir(fsPath)
	if path.Dir(fsPath) == "/" {
		parentURL += "/"
	}

	var crumbs strings.Builder
	fmt.Fprintf(&crumbs, `<a href="%s/">Lore</a>`, html.EscapeString(strings.TrimRight(lorePath, "/")))
	parts := strings.Split(strings.Trim(fsPath, "/"), "/")
	for i, part := range parts {
		crumbs.WriteString(`<span class="separator">/</span>`)
		if i == len(parts)-1 {
			fmt.Fprintf(&crumbs, `<span aria-current="page">%s</span>`, html.EscapeString(part))
			continue
		}
		crumbPath := "/" + strings.Join(parts[:i+1], "/")
		fmt.Fprintf(&crumbs, `<a href="%s%s/">%s</a>`, html.EscapeString(lorePath), html.EscapeString(crumbPath), html.EscapeString(part))
	}

	iframeURL := r.URL.EscapedPath() + "?raw=1"
	name := path.Base(fsPath)
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	fmt.Fprintf(&b, "<title>%s — OpenLore</title>", html.EscapeString(name))
	b.WriteString(`<style>*{box-sizing:border-box}html,body{height:100%;margin:0}body{display:flex;flex-direction:column;background:#0d1117;color:#c9d1d9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif}.bar{height:3rem;flex:none;display:flex;align-items:center;gap:.65rem;padding:0 .85rem;border-bottom:1px solid #30363d;background:#161b22}.breadcrumbs{display:flex;align-items:center;gap:.5rem;min-width:0;overflow:hidden;white-space:nowrap;font-size:.9rem}.breadcrumbs a{color:#58a6ff;text-decoration:none}.breadcrumbs a:hover{text-decoration:underline}.breadcrumbs span[aria-current=page]{overflow:hidden;text-overflow:ellipsis;color:#c9d1d9}.separator{color:#6e7681}.close{display:grid;place-items:center;width:2rem;height:2rem;margin-left:auto;flex:none;border-radius:6px;color:#8b949e;text-decoration:none;font-size:1.35rem;line-height:1}.close:hover{background:#30363d;color:#f0f6fc}iframe{width:100%;flex:1;border:0;background:#fff}</style></head><body>`)
	fmt.Fprintf(&b, `<nav class="bar" aria-label="File navigation"><div class="breadcrumbs">%s</div><a class="close" href="%s" aria-label="Close file and return to folder" title="Close">×</a></nav>`, crumbs.String(), html.EscapeString(parentURL))
	fmt.Fprintf(&b, `<iframe src="%s" title="%s"></iframe>`, html.EscapeString(iframeURL), html.EscapeString(name))
	b.WriteString(`</body></html>`)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(b.String()))
}

func isMarkdown(filePath string) bool {
	ext := strings.ToLower(path.Ext(filePath))
	return ext == ".md" || ext == ".markdown"
}

func renderMarkdown(w http.ResponseWriter, source []byte) error {
	var rendered bytes.Buffer
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	if err := md.Convert(source, &rendered); err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err := fmt.Fprintf(w, `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><base target="_top"><meta name="viewport" content="width=device-width, initial-scale=1"><style>:root{color-scheme:light dark}*{box-sizing:border-box}body{max-width:860px;margin:0 auto;padding:2.5rem 2rem;font:16px/1.65 -apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;color:#1f2328;background:#fff}h1,h2{border-bottom:1px solid #d0d7de;padding-bottom:.3em}h1,h2,h3,h4,h5,h6{line-height:1.25;margin:1.5em 0 .65em}h1:first-child{margin-top:0}a{color:#0969da}pre{overflow:auto;padding:1rem;border-radius:6px;background:#f6f8fa}code{font:85%% SFMono-Regular,Consolas,'Liberation Mono',monospace;background:#eff1f3;padding:.2em .4em;border-radius:4px}pre code{background:transparent;padding:0}blockquote{margin-left:0;padding-left:1em;border-left:4px solid #d0d7de;color:#59636e}img{max-width:100%%}table{border-collapse:collapse;display:block;overflow:auto}th,td{padding:.4rem .8rem;border:1px solid #d0d7de}tr:nth-child(2n){background:#f6f8fa}hr{border:0;border-top:1px solid #d0d7de}@media(prefers-color-scheme:dark){body{color:#e6edf3;background:#0d1117}a{color:#58a6ff}h1,h2,hr{border-color:#30363d}pre,code,tr:nth-child(2n){background:#161b22}blockquote{border-color:#3d444d;color:#9198a1}th,td{border-color:#3d444d}}</style></head><body>%s</body></html>`, rendered.String())
	return err
}

func (p *Passkeys) renderDir(w http.ResponseWriter, fsys vfs.FileSystem, lorePath, fsPath string) {
	entries, err := fsys.ReadDir(fsPath)
	if err != nil {
		http.Error(w, "404 not found", http.StatusNotFound)
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return entries[i].FileName < entries[j].FileName
	})

	var b strings.Builder
	b.WriteString("<!DOCTYPE html><html lang=\"en\"><head><meta charset=\"utf-8\">")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	b.WriteString("<title>OpenLore</title><style>")
	b.WriteString("*{box-sizing:border-box;margin:0;padding:0}body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#0d1117;color:#c9d1d9;padding:2rem;max-width:820px;margin:0 auto}")
	b.WriteString("h1{font-size:1.2rem;margin-bottom:1rem;color:#c9d1d9}a{color:#58a6ff;text-decoration:none}a:hover{text-decoration:underline}")
	b.WriteString("ul{list-style:none}li{padding:0.35rem 0;border-bottom:1px solid #21262d}.dir{color:#79c0ff}.crumb{color:#8b949e;margin-bottom:1.5rem;font-size:0.9rem}</style></head><body>")

	fmt.Fprintf(&b, "<h1>📜 %s</h1>", html.EscapeString(fsPath))

	if fsPath != "/" {
		parent := path.Dir(fsPath)
		fmt.Fprintf(&b, "<div class=\"crumb\"><a href=\"%s\">⬆ up</a></div>", html.EscapeString(lorePath+parent))
	}

	b.WriteString("<ul>")
	for _, e := range entries {
		child := path.Join(fsPath, e.FileName)
		href := lorePath + child
		name := html.EscapeString(e.FileName)
		if e.Dir {
			fmt.Fprintf(&b, "<li><a class=\"dir\" href=\"%s/\">%s/</a></li>", html.EscapeString(href), name)
		} else {
			fmt.Fprintf(&b, "<li><a href=\"%s\">%s</a></li>", html.EscapeString(href), name)
		}
	}
	b.WriteString("</ul></body></html>")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(b.String()))
}
