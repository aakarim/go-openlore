package cmds

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// PublishTarget is a writable docset: its logical name (the first path
// segment used in `publish /<name>/<file>`) mapped to an on-disk directory.
type PublishTarget struct {
	Name        string
	DiskDir     string
	MaxFileSize int64
}

// defaultMaxPublishSize is used when a target registers MaxFileSize <= 0.
const defaultMaxPublishSize int64 = 2_621_440 // 2.5 MiB

// Package-level publish state. Populated by the server at startup via
// RegisterPublishTarget and PublishBaseURL. See docs/openlore-cli-wart-audit.md
// (W-01/W-02) for the planned migration onto Shell/CmdContext.
var (
	PublishTargets []PublishTarget
	PublishBaseURL string
)

// RegisterPublishTarget registers a writable docset. Append-only.
func RegisterPublishTarget(name, diskDir string, maxFileSize int64) {
	PublishTargets = append(PublishTargets, PublishTarget{
		Name:        name,
		DiskDir:     diskDir,
		MaxFileSize: maxFileSize,
	})
}

// findPublishTarget returns the registered target for a docset name.
func findPublishTarget(name string) (PublishTarget, bool) {
	for _, t := range PublishTargets {
		if t.Name == name {
			return t, true
		}
	}
	return PublishTarget{}, false
}

// CmdPublish writes stdin to a file inside a writable docset. The session's
// writable docsets are supplied via the OPENLORE_DOCSETS env var (set by the
// server from the identity's `publish` list). Writes are whole-file: the
// command buffers stdin and writes once.
func CmdPublish(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	// Resolve the session's writable docsets: the intersection of the
	// identity's OPENLORE_DOCSETS and the registered publish targets.
	var writable []PublishTarget
	for _, name := range strings.Split(ctx.GetEnv("OPENLORE_DOCSETS"), ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if t, ok := findPublishTarget(name); ok {
			writable = append(writable, t)
		}
	}

	if len(writable) == 0 {
		fmt.Fprintln(errW, "No writable paths available to you.")
		return 1
	}

	if len(args) == 0 {
		fmt.Fprintln(w, "Usage: publish <path> < content")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Writable paths:")
		for _, t := range writable {
			fmt.Fprintf(w, "  /%s/\n", t.Name)
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Example: echo '# Title' | publish /knowledge/topic.md")
		return 0
	}

	// Parse /<docset>/<file...>.
	p := path.Clean("/" + strings.TrimPrefix(args[0], "/"))
	segs := strings.SplitN(strings.TrimPrefix(p, "/"), "/", 2)
	docset := segs[0]

	// The docset must be one of the session's writable targets.
	var target PublishTarget
	found := false
	for _, t := range writable {
		if t.Name == docset {
			target = t
			found = true
			break
		}
	}
	if !found {
		fmt.Fprintf(errW, "publish: no access to /%s/\n", docset)
		return 1
	}

	if len(segs) < 2 || segs[1] == "" {
		fmt.Fprintln(errW, "publish: path must include a file name after the docset")
		fmt.Fprintf(errW, "  e.g. publish /%s/my-file.md\n", docset)
		return 1
	}
	rel := segs[1]

	maxSize := target.MaxFileSize
	if maxSize <= 0 {
		maxSize = defaultMaxPublishSize
	}

	// Buffer the whole object (bounded), then commit once.
	data, err := io.ReadAll(io.LimitReader(stdin, maxSize+1))
	if err != nil {
		fmt.Fprintf(errW, "publish: %s\n", err)
		return 1
	}
	if int64(len(data)) > maxSize {
		fmt.Fprintf(errW, "publish: content exceeds maximum size of %d bytes\n", maxSize)
		return 1
	}

	dst := filepath.Join(target.DiskDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		fmt.Fprintf(errW, "publish: %s\n", err)
		return 1
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		fmt.Fprintf(errW, "publish: %s\n", err)
		return 1
	}

	fmt.Fprintf(w, "Published %s (%d bytes)\n", p, len(data))
	if PublishBaseURL != "" {
		fmt.Fprintf(w, "%s%s\n", PublishBaseURL, p)
	}
	return 0
}
