package openlore

import (
	"testing"

	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/shell/cmds"
)

func boolPtr(b bool) *bool { return &b }

// enforcedDocsetServer builds a writable, auth-enforced server with a mix of
// docset shapes for exercising the per-session builders.
func enforcedDocsetServer() *Server {
	return &Server{
		merge:        NewMergeFS(),
		authEnforced: true,
		config:       config.Config{Readonly: false}, // global write lock open
		auth: &config.AuthConfig{
			Docsets: map[string]config.DocsetSpec{
				"public": {Paths: []config.PathMapping{{Source: "/docs/public", Display: "/docs/public"}}},
				"backend": {
					Paths:          []config.PathMapping{{Source: "/docs/backend", Display: "/docs/backend"}},
					PublishDir:     "./published/backend",
					MaxPublishSize: 5000,
				},
				"archive": {
					Paths:    []config.PathMapping{{Source: "/docs/archive", Display: "/docs/archive"}},
					Readonly: boolPtr(true),
				},
				"home": {
					Paths:      []config.PathMapping{{Source: "/home/agent", Display: "/home/agent"}},
					PublishDir: "./published/home",
				},
			},
			Lore: map[string][]string{
				"agent": {"public", "backend", "archive", "home"},
			},
		},
	}
}

func docsetByName(ds []cmds.DocsetInfo, name string) (cmds.DocsetInfo, bool) {
	for _, d := range ds {
		if d.Name == name {
			return d, true
		}
	}
	return cmds.DocsetInfo{}, false
}

func TestSessionDocsets_Enforced(t *testing.T) {
	s := enforcedDocsetServer()
	id := Identity{
		IdentityName: "agent",
		LoreName:     "agent",
		HomeDocset:   "home",
		Scopes:       []string{ScopeFull},
	}

	got := s.sessionDocsets(id)

	// Order follows the lore spec.
	wantOrder := []string{"public", "backend", "archive", "home"}
	if len(got) != len(wantOrder) {
		t.Fatalf("got %d docsets, want %d: %+v", len(got), len(wantOrder), got)
	}
	for i, name := range wantOrder {
		if got[i].Name != name {
			t.Fatalf("docset[%d] = %q, want %q (order should follow lore spec)", i, got[i].Name, name)
		}
	}

	public, _ := docsetByName(got, "public")
	// public is in the lore and not per-docset readonly, so a full-scope agent
	// can write to it directly (direct writability is independent of publish_dir).
	if !public.Writable {
		t.Fatalf("public should be directly writable for a full-scope agent: %+v", public)
	}
	if public.Home || public.HasPublish {
		t.Fatalf("public should carry no attributes: %+v", public)
	}

	backend, _ := docsetByName(got, "backend")
	if !backend.Writable {
		t.Fatalf("backend should be writable")
	}
	if !backend.HasPublish {
		t.Fatalf("backend has publish_dir → HasPublish")
	}

	archive, _ := docsetByName(got, "archive")
	if archive.Writable {
		t.Fatalf("archive is per-docset readonly → not writable")
	}

	home, _ := docsetByName(got, "home")
	if !home.Home {
		t.Fatalf("home docset should be marked Home")
	}
	if !home.Writable {
		t.Fatalf("home should be writable")
	}
}

func TestSessionDocsets_GlobalReadonlyMakesAllRead(t *testing.T) {
	s := enforcedDocsetServer()
	s.config.Readonly = true // close the global lock
	id := Identity{IdentityName: "agent", LoreName: "agent", Scopes: []string{ScopeFull}}

	for _, d := range s.sessionDocsets(id) {
		if d.Writable {
			t.Fatalf("global lock closed: %q must not be writable", d.Name)
		}
	}
}

func TestSessionDocsets_AnonymousIsReadOnly(t *testing.T) {
	s := enforcedDocsetServer()
	// Anonymous: default lore, no identity name, no write scope.
	s.auth.Lore["default"] = []string{"public", "backend"}
	id := Identity{LoreName: "default"}

	for _, d := range s.sessionDocsets(id) {
		if d.Writable {
			t.Fatalf("anonymous session: %q must be read-only", d.Name)
		}
	}
}

func TestSessionPublishTargets_OnlyDocsetsWithPublishDir(t *testing.T) {
	s := enforcedDocsetServer()
	id := Identity{IdentityName: "agent", LoreName: "agent", Scopes: []string{ScopeFull}}

	targets := s.sessionPublishTargets(id)
	names := map[string]int64{}
	for _, tgt := range targets {
		names[tgt.Name] = tgt.MaxFileSize
	}
	if _, ok := names["public"]; ok {
		t.Fatalf("public has no publish_dir → not a publish target")
	}
	if _, ok := names["backend"]; !ok {
		t.Fatalf("backend has publish_dir → should be a publish target")
	}
	if names["backend"] != 5000 {
		t.Fatalf("backend max size = %d, want 5000", names["backend"])
	}
	if _, ok := names["home"]; !ok {
		t.Fatalf("home has publish_dir → should be a publish target")
	}
}

func TestSessionPublishTargets_RespectsExplicitPublishScope(t *testing.T) {
	s := enforcedDocsetServer()
	id := Identity{
		IdentityName:   "agent",
		LoreName:       "agent",
		PublishDocsets: []string{"backend"}, // explicit publish scope
		Scopes:         []string{ScopeFull},
	}

	targets := s.sessionPublishTargets(id)
	if len(targets) != 1 || targets[0].Name != "backend" {
		t.Fatalf("explicit publish scope should limit targets to backend, got %+v", targets)
	}
}

func TestSessionPublishTargets_UnenforcedHasNone(t *testing.T) {
	s := &Server{
		merge:        NewMergeFS(),
		authEnforced: false,
		config:       config.Config{Readonly: false},
		auth: &config.AuthConfig{
			Docsets: map[string]config.DocsetSpec{
				"public": {Paths: []config.PathMapping{{Source: "/", Display: "/"}}},
			},
			Lore: map[string][]string{"default": {"public"}},
		},
	}
	id := Identity{LoreName: "default", Scopes: []string{ScopeFull}}
	if targets := s.sessionPublishTargets(id); targets != nil {
		t.Fatalf("unenforced mode has no publish inboxes, got %+v", targets)
	}
}

func TestSessionDocsets_UnenforcedPublicIsWritable(t *testing.T) {
	s := &Server{
		merge:        NewMergeFS(),
		authEnforced: false,
		config:       config.Config{Readonly: false}, // writes unrestricted
		auth: &config.AuthConfig{
			Docsets: map[string]config.DocsetSpec{
				"public": {Paths: []config.PathMapping{{Source: "/", Display: "/"}}},
			},
			Lore: map[string][]string{"default": {"public"}},
		},
	}
	id := Identity{LoreName: "default", Scopes: []string{ScopeFull}}
	got := s.sessionDocsets(id)
	if len(got) != 1 || got[0].Name != "public" {
		t.Fatalf("want single public docset, got %+v", got)
	}
	if !got[0].Writable {
		t.Fatalf("unenforced public with open lock should be writable")
	}
	if got[0].Home {
		t.Fatalf("unenforced public must never be home")
	}
}
