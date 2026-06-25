package cmds

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/aakarim/go-openlore/pkg/vfs"
)

// maxAppendRetries bounds the compare-and-swap loop for atomic appends.
const maxAppendRetries = 64

// WriteFile commits data to path through the writable filesystem as a single
// atomic whole-object operation. It is the one write seam shared by every
// write verb (`>`, `>>`, tee, patch, sed -i): there is no streaming.
//
// When appendMode is false it overwrites unconditionally (last-write-wins, but
// atomic). When appendMode is true it runs a read-modify-write CAS loop so
// concurrent appends never clobber each other.
//
// It returns vfs.ErrReadOnly when the filesystem is read-only (the hard error
// surfaced to the agent), so callers can detect and message it.
func WriteFile(ctx CmdContext, p string, data []byte, appendMode bool) (string, error) {
	wfs, ok := ctx.FS().(vfs.WritableFS)
	if !ok {
		return "", vfs.ErrReadOnly
	}
	resolved := ctx.Resolve(p)

	if !appendMode {
		return wfs.WriteFileAtomic(resolved, data, vfs.WriteOpts{})
	}

	// Atomic append: read current content, append, commit-if-unchanged, retry.
	for attempt := 0; attempt < maxAppendRetries; attempt++ {
		cur, readErr := ctx.FS().ReadFile(resolved)
		exists := readErr == nil

		var opts vfs.WriteOpts
		if exists {
			h := sha256.Sum256(cur)
			hexStr := hex.EncodeToString(h[:])
			opts.IfMatch = &hexStr
		} else {
			opts.IfNoneMatch = true
		}

		combined := make([]byte, 0, len(cur)+len(data))
		combined = append(combined, cur...)
		combined = append(combined, data...)

		newHash, werr := wfs.WriteFileAtomic(resolved, combined, opts)
		if werr == nil {
			return newHash, nil
		}
		var pe *vfs.PreconditionError
		if errors.As(werr, &pe) {
			continue // raced with another writer; re-read and retry
		}
		return "", werr
	}
	return "", fmt.Errorf("append: too much write contention on %s", p)
}

// WriteFileMsg writes data and emits a uniform error line to errW on failure,
// returning a shell-style exit code (0 ok, 1 error). It centralizes the
// read-only and generic error messaging for the write verbs.
func WriteFileMsg(ctx CmdContext, errW io.Writer, cmdName, p string, data []byte, appendMode bool) int {
	if _, err := WriteFile(ctx, p, data, appendMode); err != nil {
		if errors.Is(err, vfs.ErrReadOnly) {
			fmt.Fprintf(errW, "%s: %s: read-only filesystem\n", cmdName, p)
		} else {
			fmt.Fprintf(errW, "%s: %s: %s\n", cmdName, p, err)
		}
		return 1
	}
	return 0
}
