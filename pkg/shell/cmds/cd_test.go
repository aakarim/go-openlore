package cmds_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/pkg/shell"
)

func TestCd(t *testing.T) {
	sh := shell.NewShell(testFS())
	var out, errOut bytes.Buffer
	sh.Exec("cd /docs", &out, &errOut, nil)
	out.Reset()
	sh.Exec("pwd", &out, &errOut, nil)
	if strings.TrimSpace(out.String()) != "/docs" {
		t.Errorf("cd+pwd: got %q", out.String())
	}
}

func TestCdNotFound(t *testing.T) {
	assertExitCode(t, testFS(), "cd /nonexistent", 1)
}

func TestCdNoArgsGoesToHome(t *testing.T) {
	sh := shell.NewShell(testFS())
	sh.SetEnv("HOME", "/docs")
	var out, errOut bytes.Buffer
	sh.Exec("cd /docs/sub", &out, &errOut, nil)
	sh.Exec("cd", &out, &errOut, nil)
	out.Reset()
	sh.Exec("pwd", &out, &errOut, nil)
	if strings.TrimSpace(out.String()) != "/docs" {
		t.Errorf("cd (no args) should go to $HOME: got %q", out.String())
	}
}

func TestCdNoArgsNoHomeGoesToRoot(t *testing.T) {
	sh := shell.NewShell(testFS())
	var out, errOut bytes.Buffer
	sh.Exec("cd /docs/sub", &out, &errOut, nil)
	sh.Exec("cd", &out, &errOut, nil)
	out.Reset()
	sh.Exec("pwd", &out, &errOut, nil)
	if strings.TrimSpace(out.String()) != "/" {
		t.Errorf("cd (no args, no HOME) should go to /: got %q", out.String())
	}
}

func TestEchoHomeVariable(t *testing.T) {
	sh := shell.NewShell(testFS())
	sh.SetEnv("HOME", "/docs")
	var out, errOut bytes.Buffer

	sh.Exec(`echo "$HOME"`, &out, &errOut, nil)
	if strings.TrimSpace(out.String()) != "/docs" {
		t.Errorf(`echo "$HOME" should print $HOME: got %q`, out.String())
	}

	out.Reset()
	sh.Exec("echo $HOME", &out, &errOut, nil)
	if strings.TrimSpace(out.String()) != "/docs" {
		t.Errorf("echo $HOME should print $HOME: got %q", out.String())
	}
}

func TestTildeExpansion(t *testing.T) {
	sh := shell.NewShell(testFS())
	sh.SetEnv("HOME", "/docs")
	var out, errOut bytes.Buffer

	sh.Exec("echo ~", &out, &errOut, nil)
	if strings.TrimSpace(out.String()) != "/docs" {
		t.Errorf("~ should expand to $HOME: got %q", out.String())
	}

	out.Reset()
	sh.Exec("echo ~/sub/file.txt", &out, &errOut, nil)
	if strings.TrimSpace(out.String()) != "/docs/sub/file.txt" {
		t.Errorf("~/… should expand to $HOME/…: got %q", out.String())
	}

	out.Reset()
	sh.Exec("cd ~/sub", &out, &errOut, nil)
	out.Reset()
	sh.Exec("pwd", &out, &errOut, nil)
	if strings.TrimSpace(out.String()) != "/docs/sub" {
		t.Errorf("cd ~/sub should resolve via $HOME: got %q", out.String())
	}
}

func TestTildeNotExpandedWhenQuotedOrNoHome(t *testing.T) {
	sh := shell.NewShell(testFS())
	var out, errOut bytes.Buffer

	// No HOME set: tilde stays literal.
	sh.Exec("echo ~", &out, &errOut, nil)
	if strings.TrimSpace(out.String()) != "~" {
		t.Errorf("~ with no HOME should stay literal: got %q", out.String())
	}

	// Quoted tilde stays literal even with HOME set.
	sh.SetEnv("HOME", "/docs")
	out.Reset()
	sh.Exec("echo '~'", &out, &errOut, nil)
	if strings.TrimSpace(out.String()) != "~" {
		t.Errorf("quoted ~ should stay literal: got %q", out.String())
	}
}
