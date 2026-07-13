package cmds_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/pkg/shell"
	"github.com/aakarim/go-openlore/pkg/shell/cmds"
)

// The `lore meta` command and its scanning logic live in the openlore package
// (domain logic); it plugs into this dispatcher via RegisterLoreSub. Its
// behavior is tested there. Here we only test that the generic dispatcher lets a
// plugin register a new subcommand.
func TestRegisterLoreSub_PluginCanAddSubcommand(t *testing.T) {
	cmds.RegisterLoreSub(cmds.LoreSub{
		Name:    "ping",
		Summary: "test subcommand",
		Run: func(ctx cmds.CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
			io.WriteString(w, "pong\n")
			return 0
		},
	})
	t.Cleanup(func() { cmds.DeleteLoreSubForTest("ping") })

	sh := shell.NewShell(testFS())
	var out, errOut bytes.Buffer
	code := sh.ExecPipeline("lore ping", &out, &errOut, nil)
	if code != 0 {
		t.Fatalf("registered subcommand exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "pong") {
		t.Fatalf("subcommand output = %q, want pong", out.String())
	}
}

// Bare `lore` in a cmds-only context lists the core subcommand (docsets);
// `meta` is only present when the openlore package is imported.
func TestLore_UsageListsDocsets(t *testing.T) {
	sh := shell.NewShell(testFS())
	var out bytes.Buffer
	sh.ExecPipeline("lore", &out, &out, nil)
	if !strings.Contains(out.String(), "docsets") {
		t.Fatalf("lore usage should list docsets:\n%s", out.String())
	}
}
