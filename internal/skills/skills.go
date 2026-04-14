package skills

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Skill represents a single skill command.
type Skill struct {
	Description string `json:"description"`
	File        string `json:"file"`
	Content     string `json:"-"` // loaded content
}

// Registry holds all loaded skills.
type Registry struct {
	skills map[string]*Skill
}

// NewRegistry creates an empty skill registry.
func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]*Skill)}
}

// LoadFromFS loads skills from a skills.json + markdown files in an fs.FS.
func (r *Registry) LoadFromFS(fsys fs.FS) error {
	data, err := fs.ReadFile(fsys, "skills.json")
	if err != nil {
		return nil // no skills.json is fine
	}

	var manifest struct {
		Skills map[string]*Skill `json:"skills"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parsing skills.json: %w", err)
	}

	for name, skill := range manifest.Skills {
		if skill.File != "" {
			content, err := fs.ReadFile(fsys, skill.File)
			if err != nil {
				return fmt.Errorf("reading skill %q file %q: %w", name, skill.File, err)
			}
			skill.Content = string(content)
		}
		r.skills[name] = skill
	}

	return nil
}

// LoadFromDir loads skills from a directory on disk.
func (r *Registry) LoadFromDir(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "skills.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var manifest struct {
		Skills map[string]*Skill `json:"skills"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parsing skills.json: %w", err)
	}

	for name, skill := range manifest.Skills {
		if skill.File != "" {
			content, err := os.ReadFile(filepath.Join(dir, skill.File))
			if err != nil {
				return fmt.Errorf("reading skill %q file %q: %w", name, skill.File, err)
			}
			skill.Content = string(content)
		}
		r.skills[name] = skill
	}

	return nil
}

// Get returns a skill by name.
func (r *Registry) Get(name string) (*Skill, bool) {
	s, ok := r.skills[name]
	return s, ok
}

// Names returns all registered skill names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}
	return names
}

// All returns all skills.
func (r *Registry) All() map[string]*Skill {
	return r.skills
}
