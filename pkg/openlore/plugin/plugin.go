// Package plugin defines the seven primary hook interfaces that pluggable
// openlore implementations satisfy (P1-07).
//
// Round-2 lock (open_source_plan.md §3): seven hooks, period. Engineer is
// **not** an eighth hook — it is a Go sub-interface of Processor that the
// Worker capability-detects via type assertion.
//
//	┌────────────────────────────────┐
//	│ 1. Processor    (interface)    │ — extracts artifacts from sources
//	│ 2. Retriever    (interface)    │ — answers reads beyond the VFS
//	│ 3. Connector    (interface)    │ — ingests external systems
//	│ 4. Notifier     (interface)    │ — emits change events outward
//	│ 5. AgentDirectory (interface)  │ — auth + enrolment
//	│ 6. Scorer       (interface)    │ — produces score / confidence
//	│ 7. Policy       (interface)    │ — accept / pend / reject decisions
//	└────────────────────────────────┘
//
// **No default implementations live here.** Defaults (deterministic Scorer,
// threshold Policy, file-writer Notifier, agents.yml AgentDirectory) ship
// as P2 tickets in their own packages.
package plugin

import (
	"context"
	"errors"
	"time"
)

// Confidence is the qualitative grade a Processor attaches to a Proposal.
type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

// Source is the input to a Processor — typically a file written by `kb publish`
// or by a Connector.
type Source struct {
	// Path is the virtual path of the source.
	Path string
	// Bytes is the raw content. May be lazily loaded; see ContentReader.
	Bytes []byte
	// Agent is the publishing principal, if applicable.
	Agent string
	// Partition is the partition slug, if known at processing time.
	Partition string
	// ContentHash is a content-addressed identifier for the source.
	ContentHash string
	// At is the source ingestion timestamp.
	At time.Time
	// Extra is open metadata (mime type, encoding, headers, …).
	Extra map[string]string
}

// Proposal is the unit of work emitted by a Processor. The output pipeline
// is Processor → Scorer → Policy.
type Proposal struct {
	// Kind names what the proposal represents (e.g. "topic_artifact",
	// "entity", "relationship").
	Kind string
	// Subject is the artifact key (e.g. topic slug, entity id).
	Subject string
	// Payload is the proposal body — kind-specific JSON or markdown bytes.
	Payload []byte
	// SourcePath is the virtual path that produced this proposal.
	SourcePath string
	// Confidence is the qualitative grade.
	Confidence Confidence
	// Evidence is freeform supporting context (raw spans, cross-refs, …).
	Evidence []string
	// Extra is open metadata for richer Processors.
	Extra map[string]string
}

// Score is the Scorer's enrichment of a Proposal. The Scorer is allowed to
// re-write Confidence based on its own logic.
type Score struct {
	// Numeric is the possibilistic score in [0, 1]. The default OSS Scorer
	// derives this deterministically from Confidence and evidence count.
	Numeric float64
	// Confidence may overwrite or carry through Proposal.Confidence.
	Confidence Confidence
	// Reason is a human-readable explanation for audits.
	Reason string
}

// Decision is what Policy returns. Implementations must return exactly one of
// the three concrete decisions; ambiguity is a bug.
type Decision string

const (
	DecisionAccept Decision = "accept"
	DecisionPend   Decision = "pend"
	DecisionReject Decision = "reject"
)

// Identity is the principal returned by AgentDirectory.Lookup.
type Identity struct {
	// AgentID is the canonical agent identifier (subject of `agents.yml`).
	AgentID string
	// DisplayName is a human-readable label, optional.
	DisplayName string
	// Roles is the list of role keys assigned to this agent.
	Roles []string
	// Extra is any additional metadata the directory wants to pass through.
	Extra map[string]string
}

// Credential is the input to AgentDirectory.Lookup. Exactly one of the fields
// should be set.
type Credential struct {
	// SSHFingerprint is the SHA-256 fingerprint of the public SSH key.
	SSHFingerprint string
	// PasskeyCredentialID is the WebAuthn credential id (base64url).
	PasskeyCredentialID string
	// JWT is a signed token previously issued by the directory.
	JWT string
}

// Action is the verb passed to AgentDirectory.Authorize.
type Action string

const (
	ActionRead    Action = "read"
	ActionWrite   Action = "write"
	ActionPublish Action = "publish"
	ActionAdmin   Action = "admin"
)

// ErrNotSupported is returned by AgentDirectory implementations that do not
// support enrolment (e.g., the Oiya SSO impl).
var ErrNotSupported = errors.New("not supported by this AgentDirectory")

// ----------------------------------------------------------------------
// 1. Processor
// ----------------------------------------------------------------------

// Processor extracts artifacts from a Source. Implementations:
//   - The OSS reference server ships **no default** Processor — operators
//     wire one up via a Karpathy LLM-wiki recipe, BYO, or the Oiya premium
//     processor.
//   - The Oiya impl wraps `ontology-agent/` and also implements Engineer.
type Processor interface {
	// Process turns one source into zero or more proposals. Idempotent on
	// (Source.ContentHash); calling twice with the same hash should produce
	// equivalent output.
	Process(ctx context.Context, src Source) ([]Proposal, error)
}

// Engineer is the optional, **proactive** capability of a Processor. The
// Worker capability-detects this at runtime via Go type assertion:
//
//	if eng, ok := proc.(plugin.Engineer); ok {
//	    eng.Maintain(ctx, corpus)
//	}
//
// Implementations may run schema inference, consolidation, drift detection,
// or topic synthesis. Recipe-driven Processors typically do **not** implement
// Engineer; Oiya's premium processor does.
//
// **Engineer is not a separate hook.** The hook count remains 7.
type Engineer interface {
	Processor
	Maintain(ctx context.Context, corpus Corpus) error
}

// Corpus is the read-only window the Worker hands to Engineer.Maintain. The
// shape is intentionally minimal here; richer surfaces are layered on by the
// Worker package itself.
type Corpus interface {
	ListTopics(ctx context.Context) ([]string, error)
	ListConcepts(ctx context.Context) ([]string, error)
}

// ----------------------------------------------------------------------
// 2. Retriever
// ----------------------------------------------------------------------

// Retriever answers structured retrieval queries. **No default impl in OSS.**
// Agents in OSS retrieve via `ls /topics/`, `cat`, and `grep`. Oiya plugs in
// hybrid/vector retrieval behind this interface.
type Retriever interface {
	Retrieve(ctx context.Context, q Query) ([]Hit, error)
}

// Query is a free-form retrieval request. Concrete implementations decide
// how to interpret Text vs Filters.
type Query struct {
	// Text is the user query.
	Text string
	// Partition scopes the query to a single partition; empty = all visible.
	Partition string
	// Filters is open metadata (kind, tags, time range, …).
	Filters map[string]string
	// Limit caps the number of hits. Zero means implementation default.
	Limit int
}

// Hit is a single retrieval result.
type Hit struct {
	// Path is the virtual path of the matched artifact.
	Path string
	// Snippet is the span that matched, or a relevant excerpt.
	Snippet string
	// Score is in [0, 1] from the Retriever's scoring function.
	Score float64
	// Extra is implementation metadata.
	Extra map[string]string
}

// ----------------------------------------------------------------------
// 3. Connector
// ----------------------------------------------------------------------

// Connector is an external-system ingest pipe. **No default in OSS.** A
// reference webhook impl lives at internal/connectors/webhook/ as opt-in.
// Oiya ships managed GitHub/Slack/Jira/GDrive connectors behind the same
// interface.
type Connector interface {
	// Name uniquely identifies the connector instance (matches openlore.yml).
	Name() string
	// Ingest is invoked by the Connector framework to deliver source bytes.
	// Returning an error fails the ingest; the framework decides retry.
	Ingest(ctx context.Context, src Source) error
}

// ----------------------------------------------------------------------
// 4. Notifier
// ----------------------------------------------------------------------

// Notifier emits change events outward (file, push, webhook, …). The default
// in OSS is a file writer to `events.jsonl`.
type Notifier interface {
	Notify(ctx context.Context, e Notification) error
}

// Notification is the outbound payload from the Notifier hook.
type Notification struct {
	Kind      string            `json:"kind"`
	Path      string            `json:"path,omitempty"`
	Partition string            `json:"partition,omitempty"`
	Agent     string            `json:"agent,omitempty"`
	At        time.Time         `json:"at"`
	Subject   string            `json:"subject,omitempty"`
	Body      string            `json:"body,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// ----------------------------------------------------------------------
// 5. AgentDirectory
// ----------------------------------------------------------------------

// AgentDirectory is identity lookup + authorization, plus an optional
// enrollment surface.
//
// Default in OSS: agents.yml-backed, supports SSH and passkey credentials,
// implements all enrollment methods. Oiya SSO impl returns ErrNotSupported
// for enrollment methods.
type AgentDirectory interface {
	// Lookup resolves a credential to an Identity. Returns ErrNotSupported
	// if the credential type is not handled.
	Lookup(ctx context.Context, cred Credential) (Identity, error)
	// Authorize checks whether id may perform action against partition.
	Authorize(ctx context.Context, id Identity, partition string, action Action) error
}

// AgentDirectoryEnroller is the optional enrollment surface. AgentDirectory
// implementations that do not support enrollment should not implement this
// (the HTTP layer capability-detects it via type assertion). Returning
// ErrNotSupported from individual methods is also acceptable for partial
// support.
type AgentDirectoryEnroller interface {
	Register(ctx context.Context, id Identity, cred Credential) error
	RotateCredential(ctx context.Context, agentID string, oldCred, newCred Credential) error
	Revoke(ctx context.Context, agentID string, cred Credential) error
	List(ctx context.Context) ([]Identity, error)
}

// ----------------------------------------------------------------------
// 6. Scorer
// ----------------------------------------------------------------------

// Scorer turns a Proposal into a Score.
type Scorer interface {
	Score(ctx context.Context, p Proposal) (Score, error)
}

// ----------------------------------------------------------------------
// 7. Policy
// ----------------------------------------------------------------------

// Policy decides whether a scored Proposal is accepted, pending, or rejected.
type Policy interface {
	Decide(ctx context.Context, p Proposal, s Score, partition string) (Decision, error)
}
