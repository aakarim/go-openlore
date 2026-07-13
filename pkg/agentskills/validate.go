// Package agentskills validates the agentskills.io SKILL.md format.
package agentskills

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/aakarim/go-openlore/pkg/okf"
)

var nameRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Result describes a parseable skill. Disabled skills deliberately bypass all
// strict field validation after the exact opt-out has been observed.
type Result struct{ Disabled bool }

// Validate validates content for the skill directory dirName.
func Validate(dirName string, content []byte) (Result, error) {
	fm, _, ok, err := okf.ParseFrontmatter(content)
	if err != nil {
		return Result{}, err
	}
	if !ok || fm == nil {
		return Result{}, fmt.Errorf("SKILL.md requires YAML frontmatter mapping")
	}
	if raw, ok := fm["metadata"]; ok {
		if m, ok := raw.(map[string]any); ok && m["agent_skill"] == "disable" {
			return Result{Disabled: true}, nil
		}
	}
	allowed := map[string]bool{"name": true, "description": true, "license": true, "compatibility": true, "metadata": true, "allowed-tools": true}
	for k := range fm {
		if !allowed[k] {
			return Result{}, fmt.Errorf("unknown frontmatter field %q", k)
		}
	}
	name, ok := fm["name"].(string)
	if !ok || len(name) < 1 || len(name) > 64 || !nameRE.MatchString(name) {
		return Result{}, fmt.Errorf("name must be 1-64 characters matching [a-z0-9]+(?:-[a-z0-9]+)*")
	}
	if name != path.Base(dirName) {
		return Result{}, fmt.Errorf("name %q must match parent directory %q", name, path.Base(dirName))
	}
	desc, ok := fm["description"].(string)
	if !ok || strings.TrimSpace(desc) == "" || utf8.RuneCountInString(desc) > 1024 {
		return Result{}, fmt.Errorf("description must be a nonblank string of at most 1024 characters")
	}
	for _, k := range []string{"license", "allowed-tools"} {
		if v, exists := fm[k]; exists {
			if _, ok := v.(string); !ok {
				return Result{}, fmt.Errorf("%s must be a string", k)
			}
		}
	}
	if v, exists := fm["compatibility"]; exists {
		s, ok := v.(string)
		if !ok || s == "" || utf8.RuneCountInString(s) > 500 {
			return Result{}, fmt.Errorf("compatibility must be a nonempty string of at most 500 characters")
		}
	}
	if v, exists := fm["metadata"]; exists {
		m, ok := v.(map[string]any)
		if !ok {
			return Result{}, fmt.Errorf("metadata must be a string-to-string mapping")
		}
		for k, x := range m {
			if _, ok := x.(string); !ok {
				return Result{}, fmt.Errorf("metadata.%s must be a string", k)
			}
		}
	}
	return Result{}, nil
}
