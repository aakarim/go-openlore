package cmds

import (
	"fmt"
	"io"
	"sort"
)

// SkillEntry is a registered skill command.
type SkillEntry struct {
	Name        string
	Description string
	Content     string
}

// Skills holds dynamically registered skill commands.
var Skills []SkillEntry

// RegisterSkill adds a skill as a shell command.
func RegisterSkill(name, description, content string) {
	Skills = append(Skills, SkillEntry{
		Name:        name,
		Description: description,
		Content:     content,
	})
	Register(name, makeSkillCmd(content))
}

func makeSkillCmd(content string) CmdFunc {
	return func(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
		fmt.Fprint(w, content)
		return 0
	}
}

// CmdSkills lists all available skills.
func CmdSkills(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	if len(Skills) == 0 {
		fmt.Fprintln(w, "No skills installed.")
		return 0
	}
	fmt.Fprintln(w, "Available skills:")
	fmt.Fprintln(w, "")

	sorted := make([]SkillEntry, len(Skills))
	copy(sorted, Skills)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	for _, s := range sorted {
		fmt.Fprintf(w, "  %-16s %s\n", s.Name, s.Description)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Run a skill name as a command to see its content.")
	return 0
}
