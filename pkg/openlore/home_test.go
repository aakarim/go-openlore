package openlore

import (
	"testing"

	"github.com/aakarim/go-openlore/internal/config"
)

func TestServer_ResolveHomeDir(t *testing.T) {
	s := &Server{
		auth: &config.AuthConfig{
			Docsets: map[string]config.DocsetSpec{
				"home-mapped": {Paths: []config.PathMapping{{Source: "published/agent", Display: "/home/agent"}}},
				"home-plain":  {Paths: []config.PathMapping{{Source: "/docs/agent"}}},
				"empty":       {Paths: nil},
			},
		},
	}

	cases := []struct {
		name   string
		docset string
		want   string
	}{
		{"mapped display path", "home-mapped", "/home/agent"},
		{"plain source path", "home-plain", "/docs/agent"},
		{"no home", "", ""},
		{"unknown docset", "missing", ""},
		{"docset without paths", "empty", ""},
	}
	for _, c := range cases {
		if got := s.resolveHomeDir(c.docset); got != c.want {
			t.Errorf("%s: resolveHomeDir(%q) = %q, want %q", c.name, c.docset, got, c.want)
		}
	}
}

func TestServer_ResolveHomeDir_NoAuth(t *testing.T) {
	s := &Server{}
	if got := s.resolveHomeDir("anything"); got != "" {
		t.Errorf("no auth: got %q, want empty", got)
	}
}
