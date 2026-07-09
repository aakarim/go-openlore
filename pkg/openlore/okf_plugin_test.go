package openlore

import (
	"context"
	"errors"
	"testing"

	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// runOKF composes the plugin's single admission middleware around a terminal
// handler that records whether it was reached, and drives one ChangeSet through
// it. It returns the terminal-reached flag and the chain error.
func runOKF(p *okfPlugin, cs vfs.ChangeSet) (reachedTerminal bool, err error) {
	mws := p.WriteMiddleware()
	if len(mws) != 1 {
		panic("expected exactly one OKF middleware")
	}
	terminal := func(ctx context.Context, op WriteOp) (WriteResult, error) {
		reachedTerminal = true
		return WriteResult{Hash: "ok"}, nil
	}
	h := mws[0](terminal)
	_, err = h(context.Background(), WriteOp{ChangeSet: cs})
	return reachedTerminal, err
}

const validDoc = "---\ntype: Note\n---\nbody\n"
const invalidDoc = "no frontmatter\n"

// docset builds a single-root docset spec with the given OKF config.
func docset(root string, okf *config.OKFDocsetConfig) config.DocsetSpec {
	return config.DocsetSpec{
		Paths: []config.PathMapping{{Source: root, Display: root}},
		OKF:   okf,
	}
}

// pluginWith constructs an OKF plugin over the given docsets.
func pluginWith(docsets map[string]config.DocsetSpec) *okfPlugin {
	return newOKF(docsets, nil)
}

// docsWithOKF is a docset at /docs with default OKF (enforce, *.md).
func docsWithOKF() map[string]config.DocsetSpec {
	return map[string]config.DocsetSpec{"docs": docset("/docs", &config.OKFDocsetConfig{})}
}

func TestOKFPlugin_RejectsNonConformantMarkdown(t *testing.T) {
	p := pluginWith(docsWithOKF()) // defaults: *.md, enforce=true
	reached, err := runOKF(p, writeCSBytes("/docs/bad.md", invalidDoc))
	if err == nil {
		t.Fatal("expected rejection error for non-conformant .md write")
	}
	if reached {
		t.Fatal("terminal handler should not be reached when a write is rejected")
	}
}

func TestOKFPlugin_AllowsConformantMarkdown(t *testing.T) {
	p := pluginWith(docsWithOKF())
	reached, err := runOKF(p, writeCSBytes("/docs/good.md", validDoc))
	if err != nil {
		t.Fatalf("unexpected error for conformant .md write: %v", err)
	}
	if !reached {
		t.Fatal("terminal handler should be reached for a conformant write")
	}
}

func TestOKFPlugin_IgnoresNonMatchingFiles(t *testing.T) {
	p := pluginWith(docsWithOKF())
	// A .txt file with garbage frontmatter is not an OKF concern by default.
	reached, err := runOKF(p, writeCSBytes("/docs/notes.txt", invalidDoc))
	if err != nil {
		t.Fatalf("non-matching file should pass through, got %v", err)
	}
	if !reached {
		t.Fatal("terminal handler should be reached for a non-matching file")
	}
}

func TestOKFPlugin_NonEnforcingAllowsButPasses(t *testing.T) {
	p := pluginWith(map[string]config.DocsetSpec{
		"docs": docset("/docs", &config.OKFDocsetConfig{Enforce: boolPtr(false)}),
	})
	reached, err := runOKF(p, writeCSBytes("/docs/bad.md", invalidDoc))
	if err != nil {
		t.Fatalf("non-enforcing plugin must not reject, got %v", err)
	}
	if !reached {
		t.Fatal("terminal handler should be reached in non-enforcing mode")
	}
}

func TestOKFPlugin_CustomPatterns(t *testing.T) {
	p := pluginWith(map[string]config.DocsetSpec{
		"docs": docset("/docs", &config.OKFDocsetConfig{Patterns: []string{"*.markdown"}}),
	})

	// .md is no longer matched → passes through unchecked.
	reached, err := runOKF(p, writeCSBytes("/docs/bad.md", invalidDoc))
	if err != nil || !reached {
		t.Fatalf(".md should be ignored with custom patterns (err=%v reached=%v)", err, reached)
	}

	// .markdown is now matched → rejected.
	reached, err = runOKF(p, writeCSBytes("/docs/bad.markdown", invalidDoc))
	if err == nil {
		t.Fatal("expected rejection for non-conformant .markdown write")
	}
	if reached {
		t.Fatal("terminal should not be reached for rejected .markdown write")
	}
}

func TestOKFPlugin_NonWriteActionsPassThrough(t *testing.T) {
	p := pluginWith(docsWithOKF())
	for _, action := range []vfs.ChangeAction{
		vfs.ChangeActionMkdir,
		vfs.ChangeActionMkdirAll,
		vfs.ChangeActionRemove,
		vfs.ChangeActionRemoveAll,
	} {
		reached, err := runOKF(p, vfs.ChangeSet{Target: "/docs/x.md", Action: action})
		if err != nil {
			t.Fatalf("action %q should pass through, got %v", action, err)
		}
		if !reached {
			t.Fatalf("terminal should be reached for action %q", action)
		}
	}
}

func TestOKFPlugin_ReservedFilesLenient(t *testing.T) {
	p := pluginWith(docsWithOKF())
	// index.md without frontmatter is conformant (reserved file).
	reached, err := runOKF(p, writeCSBytes("/docs/index.md", "# Section\n* [x](x.md)\n"))
	if err != nil || !reached {
		t.Fatalf("reserved index.md should pass (err=%v reached=%v)", err, reached)
	}
}

// A write outside any OKF-carrying docset is never validated.
func TestOKFPlugin_PathOutsideDocsetNotValidated(t *testing.T) {
	p := pluginWith(docsWithOKF())
	reached, err := runOKF(p, writeCSBytes("/other/bad.md", invalidDoc))
	if err != nil || !reached {
		t.Fatalf("write outside the OKF docset must pass (err=%v reached=%v)", err, reached)
	}
}

// A docset without OKF config never validates its writes.
func TestOKFPlugin_DocsetWithoutOKFNotValidated(t *testing.T) {
	p := pluginWith(map[string]config.DocsetSpec{
		"adil": docset("/adil", nil),
	})
	reached, err := runOKF(p, writeCSBytes("/adil/bad.md", invalidDoc))
	if err != nil || !reached {
		t.Fatalf("docset without OKF must not validate (err=%v reached=%v)", err, reached)
	}
}

// A nested docset carrying OKF validates only its subtree; the parent docset
// (no OKF) is untouched. This is the include-via-nesting case: /adil is exempt,
// /adil/wiki is enforced.
func TestOKFPlugin_NestedDocsetIncludesSubtree(t *testing.T) {
	p := pluginWith(map[string]config.DocsetSpec{
		"adil":      docset("/adil", nil),
		"adil-wiki": docset("/adil/wiki", &config.OKFDocsetConfig{}),
	})

	// Parent subtree: not validated.
	reached, err := runOKF(p, writeCSBytes("/adil/notes.md", invalidDoc))
	if err != nil || !reached {
		t.Fatalf("/adil (no OKF) must not validate (err=%v reached=%v)", err, reached)
	}

	// Nested subtree: validated (longest matching root wins).
	reached, err = runOKF(p, writeCSBytes("/adil/wiki/page.md", invalidDoc))
	if err == nil {
		t.Fatal("/adil/wiki (OKF) must reject non-conformant write")
	}
	if reached {
		t.Fatal("terminal should not be reached for rejected nested write")
	}
}

// A nested docset without OKF shadows a parent's OKF: the exclude-via-nesting
// case. /adil is enforced, /adil/drafts is exempt.
func TestOKFPlugin_NestedDocsetExcludesSubtree(t *testing.T) {
	p := pluginWith(map[string]config.DocsetSpec{
		"adil":        docset("/adil", &config.OKFDocsetConfig{}),
		"adil-drafts": docset("/adil/drafts", nil),
	})

	// Parent subtree: validated.
	reached, err := runOKF(p, writeCSBytes("/adil/notes.md", invalidDoc))
	if err == nil {
		t.Fatal("/adil (OKF) must reject non-conformant write")
	}
	if reached {
		t.Fatal("terminal should not be reached for rejected parent write")
	}

	// Nested subtree without OKF: exempt (longest matching root has no OKF).
	reached, err = runOKF(p, writeCSBytes("/adil/drafts/page.md", invalidDoc))
	if err != nil || !reached {
		t.Fatalf("/adil/drafts (no OKF) must be exempt (err=%v reached=%v)", err, reached)
	}
}

func TestOKFPlugin_ImplementsProvider(t *testing.T) {
	var _ WriteMiddlewareProvider = pluginWith(docsWithOKF())
}

// Guard against an unexpected error type leaking (should be a plain wrapped
// error, not a PendingChangeError which would be mis-read as "pending").
func TestOKFPlugin_RejectionIsHardError(t *testing.T) {
	p := pluginWith(docsWithOKF())
	_, err := runOKF(p, writeCSBytes("/docs/bad.md", invalidDoc))
	var pce *vfs.PendingChangeError
	if errors.As(err, &pce) {
		t.Fatal("OKF rejection must be a hard error, not a PendingChangeError")
	}
}

func TestAnyDocsetHasOKF(t *testing.T) {
	if anyDocsetHasOKF(map[string]config.DocsetSpec{"a": docset("/a", nil)}) {
		t.Fatal("no docset carries OKF; expected false")
	}
	if !anyDocsetHasOKF(docsWithOKF()) {
		t.Fatal("a docset carries OKF; expected true")
	}
}
