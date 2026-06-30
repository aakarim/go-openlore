package cmds

import (
	"fmt"
	"io"
	"strings"
)

// ApprovalBackend resolves human-gated write requests (Part C). The server
// supplies the implementation (it owns the request store and the writable
// substrate); the approve/reject commands are thin front-ends that read the
// approver's identity and capabilities from the session env and delegate here.
type ApprovalBackend interface {
	// Approve commits the request's proposed write via CAS. msg is the
	// user-facing result line. err is only for unexpected failures; an
	// authorization denial or a stale precondition is reported via msg with a
	// non-nil err so the command can exit non-zero while still printing msg.
	Approve(id, approver string, capabilities []string) (msg string, err error)
	// Reject marks the request rejected without writing anything.
	Reject(id, approver string, capabilities []string) (msg string, err error)
}

// Approvals is the active approval backend, set by the server at startup. Nil
// when the server has no approval control plane (no data_dir / no rules).
var Approvals ApprovalBackend

func sessionCapabilities(ctx CmdContext) []string {
	raw := ctx.GetEnv("OPENLORE_CAPABILITIES")
	var caps []string
	for _, c := range strings.Split(raw, ",") {
		if c = strings.TrimSpace(c); c != "" {
			caps = append(caps, c)
		}
	}
	return caps
}

// CmdApprove approves a pending write request, committing it through CAS.
func CmdApprove(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	return runApproval(ctx, args, w, errW, "approve")
}

// CmdReject rejects a pending write request without writing anything.
func CmdReject(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	return runApproval(ctx, args, w, errW, "reject")
}

func runApproval(ctx CmdContext, args []string, w io.Writer, errW io.Writer, verb string) int {
	if Approvals == nil {
		fmt.Fprintf(errW, "%s: approvals are not enabled on this server\n", verb)
		return 1
	}
	if len(args) != 1 || args[0] == "" {
		fmt.Fprintf(errW, "Usage: %s <request-id>\n", verb)
		return 1
	}
	id := args[0]
	approver := ctx.GetEnv("OPENLORE_IDENTITY")
	caps := sessionCapabilities(ctx)

	var msg string
	var err error
	if verb == "approve" {
		msg, err = Approvals.Approve(id, approver, caps)
	} else {
		msg, err = Approvals.Reject(id, approver, caps)
	}
	if err != nil {
		if msg != "" {
			fmt.Fprintln(errW, msg)
		}
		return 1
	}
	if msg != "" {
		fmt.Fprintln(w, msg)
	}
	return 0
}
