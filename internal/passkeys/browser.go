package passkeys

import (
	"fmt"
	"html"
	"mime"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/aakarim/go-openlore/pkg/vfs"
)

// LoreBrowserHandler serves an authenticated web browser over the filesystem.
// Unauthenticated requests are redirected to the passkey login page. The set of
// paths a session may view is restricted to the docsets granted by the
// session's lore spec.
func (p *Passkeys) LoreBrowserHandler(fsys vfs.FileSystem) http.Handler {
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

		allowed := p.allowedPrefixes(session.Identity)

		// Map the request path under lorePath onto a filesystem path.
		rel := strings.TrimPrefix(r.URL.Path, lorePath)
		if rel == "" {
			http.Redirect(w, r, lorePath+"/", http.StatusFound)
			return
		}
		fsPath := vfs.CleanPath(rel)

		if !pathAllowed(fsPath, allowed) {
			http.Error(w, "403 forbidden", http.StatusForbidden)
			return
		}

		info, err := fsys.Stat(fsPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		if info.Dir {
			p.renderDir(w, fsys, lorePath, fsPath, allowed)
			return
		}

		data, err := fsys.ReadFile(fsPath)
		if err != nil {
			http.NotFound(w, r)
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

// allowedPrefixes returns the display path prefixes an identity may browse,
// resolved from the docsets it holds any grant on.
func (p *Passkeys) allowedPrefixes(identity string) []string {
	if p.auth == nil {
		return []string{"/"}
	}
	var grants map[string]string
	for _, ident := range p.auth.Identities {
		if ident.Name == identity {
			grants = ident.Docsets
			break
		}
	}
	if grants == nil {
		return nil
	}
	var prefixes []string
	for name := range grants {
		spec, ok := p.auth.Docsets[name]
		if !ok {
			continue
		}
		for _, pm := range spec.Paths {
			disp := pm.Display
			if disp == "" {
				disp = pm.Source
			}
			prefixes = append(prefixes, vfs.CleanPath(disp))
		}
	}
	return prefixes
}

func pathAllowed(p string, allowed []string) bool {
	for _, pref := range allowed {
		if pref == "/" {
			return true
		}
		if p == pref || strings.HasPrefix(p, pref+"/") {
			return true
		}
		// Allow browsing ancestor directories of an allowed prefix so the
		// listing can show the way down to it.
		if strings.HasPrefix(pref, p+"/") || p == "/" {
			return true
		}
	}
	return false
}

func (p *Passkeys) renderDir(w http.ResponseWriter, fsys vfs.FileSystem, lorePath, fsPath string, allowed []string) {
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
		if !pathAllowed(child, allowed) {
			continue
		}
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
