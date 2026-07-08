package cmds_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/pkg/shell"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// pendingFS wraps mapFS so every mutation is parked as a pending change (as an
// admission middleware would), letting us assert the write verbs treat
// *vfs.PendingChangeError as exit-0 + a Ref line.
type pendingFS struct {
	*mapFS
	ref string
}

func (p *pendingFS) WriteFileAtomic(path string, _ []byte, _ vfs.WriteOpts) (string, error) {
	return "", &vfs.PendingChangeError{Ref: p.ref, ChangeSet: vfs.ChangeSet{Target: path, Action: vfs.ChangeActionWrite}}
}
func (p *pendingFS) Remove(path string) error {
	return &vfs.PendingChangeError{Ref: p.ref, ChangeSet: vfs.ChangeSet{Target: path, Action: vfs.ChangeActionRemove}}
}
func (p *pendingFS) RemoveAll(path string, _ vfs.RemoveOpts) error {
	return &vfs.PendingChangeError{Ref: p.ref, ChangeSet: vfs.ChangeSet{Target: path, Action: vfs.ChangeActionRemoveAll}}
}

func execPending(fs vfs.WritableFS, cmd string) (string, string, int) {
	sh := shell.NewShell(fs)
	var out, errOut bytes.Buffer
	code := sh.ExecPipeline(cmd, &out, &errOut, nil)
	return out.String(), errOut.String(), code
}

func TestWritePendingChangeIsExitZeroWithRef(t *testing.T) {
	fs := &pendingFS{mapFS: testFS(), ref: "chg-42"}

	// A redirect write parked as pending → exit 0, Ref surfaced on stderr.
	out, errOut, code := execPending(fs, "echo hi > /docs/new.txt")
	if code != 0 {
		t.Fatalf("write: code=%d out=%q err=%q", code, out, errOut)
	}
	if !strings.Contains(errOut, "chg-42") || !strings.Contains(errOut, "pending") {
		t.Fatalf("write pending line missing Ref: %q", errOut)
	}
}

func TestRmPendingChangeIsExitZeroWithRef(t *testing.T) {
	fs := &pendingFS{mapFS: testFS(), ref: "chg-99"}

	// rm of an existing file parked as pending → exit 0, Ref surfaced on stdout.
	out, errOut, code := execPending(fs, "rm /docs/notes.txt")
	if code != 0 {
		t.Fatalf("rm: code=%d out=%q err=%q", code, out, errOut)
	}
	if !strings.Contains(out, "chg-99") || !strings.Contains(out, "pending") {
		t.Fatalf("rm pending line missing Ref: %q", out)
	}
}
