package cmds_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/pkg/shell"
)

func TestJqField(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "jq -r .name /docs/data.json")
	if strings.TrimSpace(out) != "alice" {
		t.Errorf("jq: got %q", strings.TrimSpace(out))
	}
}

func TestJqPipe(t *testing.T) {
	sh := shell.NewShell(testFS())
	var out, errOut bytes.Buffer
	sh.Exec("jq '.items | length' /docs/data.json", &out, &errOut, nil)
	if strings.TrimSpace(out.String()) != "3" {
		t.Errorf("jq pipe: got %q", out.String())
	}
}

func TestJqSelect(t *testing.T) {
	sh := shell.NewShell(testFS())
	var out, errOut bytes.Buffer
	sh.Exec("jq '.[] | select(.active)' /docs/users.json", &out, &errOut, nil)
	if !strings.Contains(out.String(), "alice") {
		t.Errorf("jq select: got %q", out.String())
	}
	if strings.Contains(out.String(), "bob") {
		t.Error("jq select should filter bob")
	}
}

func TestJqMap(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "jq 'map(.name)' /docs/users.json")
	if !strings.Contains(out, "alice") {
		t.Errorf("jq map: got %q", out)
	}
}

func TestJqObjectConstruct(t *testing.T) {
	sh := shell.NewShell(testFS())
	var out, errOut bytes.Buffer
	sh.Exec("jq '{n: .name, a: .age}' /docs/data.json", &out, &errOut, nil)
	if !strings.Contains(out.String(), "alice") || !strings.Contains(out.String(), "30") {
		t.Errorf("jq object: got %q", out.String())
	}
}

func TestJqSortBy(t *testing.T) {
	sh := shell.NewShell(testFS())
	var out bytes.Buffer
	sh.Exec("jq 'sort_by(.age)' /docs/users.json", &out, &bytes.Buffer{}, nil)
	bobIdx := strings.Index(out.String(), "bob")
	aliceIdx := strings.Index(out.String(), "alice")
	if bobIdx < 0 || aliceIdx < 0 || bobIdx > aliceIdx {
		t.Errorf("jq sort_by: bob should come before alice, got %q", out.String())
	}
}

func TestJqAdd(t *testing.T) {
	sh := shell.NewShell(testFS())
	var out bytes.Buffer
	sh.Exec("jq '.items | add' /docs/data.json", &out, &bytes.Buffer{}, nil)
	if strings.TrimSpace(out.String()) != "6" {
		t.Errorf("jq add: got %q", strings.TrimSpace(out.String()))
	}
}
