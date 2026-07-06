package openlore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aakarim/go-openlore/assets"
	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/internal/httpserver"
	"github.com/aakarim/go-openlore/internal/legal"
	"github.com/aakarim/go-openlore/internal/metrics"
	"github.com/aakarim/go-openlore/internal/passkeys"
	"github.com/aakarim/go-openlore/internal/skills"
	"github.com/aakarim/go-openlore/pkg/openlore/eventbus"
	"github.com/aakarim/go-openlore/pkg/openlore/hooks"
	"github.com/aakarim/go-openlore/pkg/shell"
	"github.com/aakarim/go-openlore/pkg/shell/cmds"
	"github.com/aakarim/go-openlore/pkg/vfs"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/logging"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

// SessionFSFn returns the filesystem to use for a given SSH session identity.
// The default implementation returns the base FS unchanged.
type SessionFSFn func(id Identity, base vfs.FileSystem) vfs.FileSystem

// Server is the main OpenLore SSH server.
type Server struct {
	config config.Config
	// auth is always non-nil so downstream code never nil-checks it. When
	// authEnforced is false, auth is an empty policy and the server runs in
	// trusted/unrestricted mode (local `openlore .`, or an embedded server such
	// as knowledge-backend that does its own scoping): callers get full access.
	// When true, an access-control policy was loaded and identity scoping is
	// enforced (docsets filtered, anonymous is read-only).
	auth         *config.AuthConfig
	authEnforced bool
	fs           vfs.FileSystem
	merge        *MergeFS
	metrics      *metrics.Metrics
	srv          *ssh.Server
	httpSrv      *httpserver.Server
	passkeys     *passkeys.Passkeys
	logger       *slog.Logger
	motd         string

	onConnect    OnConnectFunc
	onDisconnect OnDisconnectFunc
	sessionFSFn  SessionFSFn

	// requests is the control-plane store for human-gated write requests
	// (Part C). Served read-only at /requests.
	requests *RequestStore

	// bus is the storage-event bus. Write events (post_write) and approval
	// events (approval_pending) fan out to configured shell hooks.
	bus *eventbus.Bus

	// jobs runs async external work (Part D), surfaced read-only at /jobs.
	jobs *JobManager

	// Bearer-token auth for the MCP + HTTP API (docs/mcp-bearer-auth.md).
	// identityStore is always set (resolves claims → Identity). The rest are
	// non-nil only when auth.tokens is configured, which enables token auth.
	identityStore IdentityStore
	issuer        Issuer
	refreshStore  RefreshTokenStore
	clientStore   ClientStore
	authCodes     *authCodeStore
	authorizeReqs *authorizeStore
	tokens        *tokenEndpoint
	// oidc verifies external IdP assertions for the jwt-bearer (WIF) grant. It
	// is non-nil only when oidc_issuers are configured alongside auth.tokens.
	oidc OIDCVerifier
}

// NewServer creates a new OpenLore SSH server.
// rootDir is the primary directory to serve (can be empty if using Mount).
// Options are applied using the functional options pattern via config.Option.
func NewServer(rootDir string, opts ...config.Option) (*Server, error) {
	cfg, err := config.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	s := &Server{
		config: cfg,
		// Default to an empty (unenforced) policy; a loaded auth file replaces
		// it and sets authEnforced below.
		auth:    &config.AuthConfig{},
		merge:   NewMergeFS(),
		metrics: &metrics.Metrics{},
		logger:  logger,
		motd:    cfg.MOTD,
		bus:     eventbus.New(logger),
	}

	// Wire configured shell hooks (post_write, approval_pending, …) onto the
	// bus. Empty hook config is valid — nothing fires.
	hooks.Subscribe(s.bus, hooks.Config{DataDir: cfg.DataDir, Hooks: cfg.Hooks}, nil, logger)

	// Load auth config
	if cfg.AuthFile != "" {
		auth, err := config.LoadAuthConfig(cfg.AuthFile)
		if err != nil {
			return nil, fmt.Errorf("loading auth config: %w", err)
		}
		s.auth = auth
		s.authEnforced = true

		// Auth policy fields override config defaults
		if auth.AllowKeyless != nil {
			s.config.AllowKeyless = *auth.AllowKeyless
		}
		if auth.UnknownIdentity != "" {
			s.config.UnknownIdentity = auth.UnknownIdentity
		}
		if auth.DefaultCwd != "" {
			s.config.DefaultCwd = auth.DefaultCwd
		}

		// Register writable docsets for the publish command
		for name, ds := range auth.Docsets {
			if ds.PublishDir != "" {
				cmds.RegisterPublishTarget(name, ds.MaxPublishSize)
			}
		}
	}

	// Collect docset root display paths so DirFS.Mkdir can enforce the
	// "strictly below a docset root" boundary.
	var docsetRoots []string
	for _, ds := range s.auth.Docsets {
		for _, pm := range ds.Paths {
			display := pm.Display
			if display == "" {
				display = pm.Source
			}
			docsetRoots = append(docsetRoots, display)
		}
	}

	// Set up root directory
	if rootDir != "" {
		rootFS := NewDirFS(rootDir, cfg.Files).WithDocsetRoots(docsetRoots).WithBus(s.bus)
		s.merge.SetRoot(rootFS)
	}

	// Set up additional folders. Each folder mount is itself a docset, so any
	// non-root path within it is a valid Mkdir target (default boundary).
	for _, folder := range cfg.Folders {
		folderFS := NewDirFS(folder.Path, cfg.Files).WithBus(s.bus)
		s.merge.Mount(folder.Name, folderFS)
	}

	// Control plane: the human-gated-write request store (Part C), served
	// read-only at /requests. Opt-in via data_dir — without it, the approval
	// control plane is absent (the embedded KB server, local `openlore .`).
	// The mount is a system mount, so it is visible to every session regardless
	// of lore; writes to it are denied (RequestsFS is not a WritableFS).
	if cfg.DataDir != "" {
		store, err := NewRequestStore(filepath.Join(cfg.DataDir, "requests"))
		if err != nil {
			return nil, fmt.Errorf("initializing request store: %w", err)
		}
		s.requests = store
		s.merge.MountSystem("requests", NewRequestsFS(store))
		// The approve/reject commands resolve requests by replaying the
		// proposed write through the raw substrate (s.merge), bypassing
		// per-session scope/approval wrappers — the approval is the authority.
		cmds.Approvals = &approvalBackend{store: store, commitFS: s.merge}
	}

	// Enable the experimental writable substrate when the global lock is open.
	// Fail fast if writes were requested but no backend can support them.
	if !cfg.Readonly {
		if err := s.merge.SetWriteable(); err != nil {
			return nil, fmt.Errorf("enabling writable mode (readonly=false): %w", err)
		}
		logger.Info("writable substrate enabled (readonly=false)")

		// Async external work (Part D): the `spawn` command runs a command in a
		// bounded goroutine and writes its stdout back through the captured
		// scoped FS. Only meaningful when writes are possible. Jobs are in-memory
		// (lost on restart) and surfaced read-only at /jobs.
		s.jobs = NewJobManager(cfg.MaxJobs, hooks.ShellRunner{}, s.bus, logger)
		cmds.Jobs = s.jobs
		s.merge.MountSystem("jobs", NewJobsFS(s.jobs))
	}

	// Load skills
	skillReg := skills.NewRegistry()

	// Load embedded skills
	if embSkills := assets.Skills(); embSkills != nil {
		if err := skillReg.LoadFromFS(embSkills); err != nil {
			return nil, fmt.Errorf("loading embedded skills: %w", err)
		}
	}

	// Load runtime skills from directory
	if cfg.SkillsDir != "" {
		if err := skillReg.LoadFromDir(cfg.SkillsDir); err != nil {
			return nil, fmt.Errorf("loading skills from %s: %w", cfg.SkillsDir, err)
		}
	}

	// Register skills as shell commands
	for name, skill := range skillReg.All() {
		cmds.RegisterSkill(name, skill.Description, skill.Content)
	}

	s.fs = s.merge

	// Set up passkeys if enabled
	if cfg.Passkeys.Enabled {
		pkFile := cfg.Passkeys.PasskeysFile
		if pkFile == "" {
			pkFile = "./config/passkeys.json"
		}
		rpName := cfg.Passkeys.RPName
		if rpName == "" {
			rpName = "OpenLore"
		}
		sessionTTL := 24 * time.Hour
		if cfg.Passkeys.SessionTTL != "" {
			if d, err := time.ParseDuration(cfg.Passkeys.SessionTTL); err == nil {
				sessionTTL = d
			}
		}

		// Read host key material for session signing
		sessionKey := []byte("openlore-default-session-key")
		if keyData, err := os.ReadFile(cfg.HostKeyPath); err == nil {
			sessionKey = keyData
		}

		pk, err := passkeys.New(passkeys.Config{
			Enabled:      true,
			RPID:         cfg.Passkeys.RPID,
			RPName:       rpName,
			RPOrigins:    cfg.Passkeys.RPOrigins,
			LorePath:     cfg.Passkeys.LorePath,
			PasskeysFile: pkFile,
			SessionTTL:   sessionTTL,
		}, sessionKey, logger)
		if err != nil {
			return nil, fmt.Errorf("setting up passkeys: %w", err)
		}
		s.passkeys = pk

		pk.SetAuthConfig(s.auth)
	}

	if err := s.initAuth(); err != nil {
		return nil, fmt.Errorf("setting up token auth: %w", err)
	}

	return s, nil
}

// Config returns the resolved configuration.
func (s *Server) Config() config.Config {
	return s.config
}

// Mount adds a named filesystem mount point using a vfs.FileSystem.
func (s *Server) Mount(name string, fs vfs.FileSystem) {
	s.merge.Mount(name, fs)
}

// MountFS adds a named filesystem mount point using a standard fs.FS.
func (s *Server) MountFS(name string, fsys fs.FS) {
	s.merge.Mount(name, NewFSAdapter(fsys))
}

// SetRootFS sets the root filesystem using a standard fs.FS.
func (s *Server) SetRootFS(fsys fs.FS) {
	s.merge.SetRoot(NewFSAdapter(fsys))
}

// SetRootBashFS sets the root filesystem using a vfs.FileSystem. Paths
// that don't match any mount fall through to this filesystem.
func (s *Server) SetRootBashFS(fsys vfs.FileSystem) {
	s.merge.SetRoot(fsys)
}

// SetSessionFSFn registers a per-session filesystem decorator. When set,
// the server calls fn(identity, baseFS) for each new SSH session and uses
// the returned filesystem for that session's shell.
func (s *Server) SetSessionFSFn(fn SessionFSFn) {
	s.sessionFSFn = fn
}

func (s *Server) advertisedSSHPort() int {
	if s.config.ExternalSSHPort != 0 {
		return s.config.ExternalSSHPort
	}
	return s.config.Port
}

// OnConnect registers a callback for new connections.
func (s *Server) OnConnect(fn OnConnectFunc) {
	s.onConnect = fn
}

// OnDisconnect registers a callback for disconnections.
func (s *Server) OnDisconnect(fn OnDisconnectFunc) {
	s.onDisconnect = fn
}

// FileSystem returns the server's filesystem.
func (s *Server) FileSystem() vfs.FileSystem {
	return s.fs
}

func generateSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) resolveIdentity(sess ssh.Session) Identity {
	id := Identity{
		RemoteAddr:  sess.RemoteAddr().String(),
		User:        sess.User(),
		PublicKey:   sess.PublicKey(),
		SessionID:   generateSessionID(),
		ConnectedAt: time.Now(),
	}

	// Resolve path access from auth config
	if s.authEnforced && id.PublicKey != nil {
		// Extract the underlying public key for matching.
		// If the client presented a certificate, sess.PublicKey() returns
		// the certificate itself — we need the inner key to match against
		// raw public keys stored in lore.json identities.
		matchKey := id.PublicKey
		var cert *gossh.Certificate
		if c, ok := id.PublicKey.(*gossh.Certificate); ok {
			cert = c
			matchKey = c.Key
		}

		keyStr := string(gossh.MarshalAuthorizedKey(matchKey))
		matched := false

		// First: try matching by public key.
		for _, ident := range s.auth.Identities {
			if ident.PublicKey == keyStr {
				// Shared with token resolution: an authenticated key holds this
				// identity's full authority.
				src := s.identityFromAuth(ident)
				id.IdentityName = src.IdentityName
				id.LoreName = src.LoreName
				id.PathAccess = src.PathAccess
				id.PublishDocsets = src.PublishDocsets
				id.Capabilities = src.Capabilities
				id.HomeDir = src.HomeDir
				id.Scopes = src.Scopes
				matched = true
				break
			}
		}

		// Second: if the client used a certificate and no key matched,
		// map certificate principals to lore names. This lets you
		// sign certs with `-n frontend` and have them automatically get
		// the "frontend" lore without registering individual keys.
		if !matched && cert != nil {
			for _, principal := range cert.ValidPrincipals {
				if _, ok := s.auth.Lore[principal]; ok {
					id.LoreName = principal
					id.PathAccess = s.resolveLorePathAccess(principal)
					// A valid cert holds this lore's full authority.
					id.Scopes = []string{ScopeFull}
					matched = true
					break
				}
			}
		}

		// Unrecognized key/cert: fall back to "default" lore
		if !matched {
			if _, ok := s.auth.Lore["default"]; ok {
				id.LoreName = "default"
				id.PathAccess = s.resolveLorePathAccess("default")
			}
		}
	} else if s.authEnforced {
		// Keyless connection with auth config: use "default" lore
		if _, ok := s.auth.Lore["default"]; ok {
			id.LoreName = "default"
			id.PathAccess = s.resolveLorePathAccess("default")
		}
	} else {
		// No auth config at all: full access
		id.Scopes = []string{ScopeFull}
		for name := range s.merge.mounts {
			id.PathAccess = append(id.PathAccess, config.PathMapping{
				Source:  "/" + name,
				Display: "/" + name,
			})
		}
	}

	return id
}

// resolveLorePathAccess resolves a lore name to path mappings by looking up
// its docset list and collecting all paths from each referenced docset.
func (s *Server) resolveLorePathAccess(loreName string) []config.PathMapping {
	docsetNames, ok := s.auth.Lore[loreName]
	if !ok {
		return nil
	}
	var mappings []config.PathMapping
	for _, dsName := range docsetNames {
		if ds, ok := s.auth.Docsets[dsName]; ok {
			for _, pm := range ds.Paths {
				mappings = append(mappings, pm)
			}
		}
	}
	return mappings
}

// resolveHomeDir returns the display path of the named home docset, used as
// the session's $HOME and initial working directory. It uses the first path
// mapping of the docset (its Display, falling back to Source). Returns "" when
// no home is set or the docset has no paths.
func (s *Server) resolveHomeDir(homeDocset string) string {
	if homeDocset == "" {
		return ""
	}
	ds, ok := s.auth.Docsets[homeDocset]
	if !ok || len(ds.Paths) == 0 {
		return ""
	}
	pm := ds.Paths[0]
	display := pm.Display
	if display == "" {
		display = pm.Source
	}
	return vfs.CleanPath(display)
}

// writableDocsetRoots resolves the docset roots an identity may write to
// (Part B). It uses the identity's explicit publish list, falling back to every
// docset in its lore when no explicit publish scope is set (the admin /
// no-publish case). Docsets flagged readonly are excluded — a per-docset lock
// can only further restrict. Returns nil for an unrecognized identity, so
// anonymous sessions are read-only.
func (s *Server) writableDocsetRoots(id Identity) []string {
	if id.IdentityName == "" {
		return nil
	}
	names := id.PublishDocsets
	if len(names) == 0 {
		names = s.auth.Lore[id.LoreName]
	}
	var roots []string
	for _, name := range names {
		ds, ok := s.auth.Docsets[name]
		if !ok {
			continue
		}
		if ds.Readonly != nil && *ds.Readonly {
			continue // per-docset lock excludes it from the writable scope
		}
		for _, pm := range ds.Paths {
			display := pm.Display
			if display == "" {
				display = pm.Source
			}
			roots = append(roots, display)
		}
	}
	return roots
}

// writeConflictPolicy resolves the policy governing whole-file overwrites to
// virtual path p. A per-docset override (DocsetSpec.WriteConflictPolicy) wins
// for paths inside that docset; otherwise the global default applies. An
// invalid per-docset value is ignored in favor of the global default.
func (s *Server) writeConflictPolicy(p string) vfs.WriteConflictPolicy {
	global := s.config.WriteConflictPolicy
	if global == "" {
		global = vfs.DefaultWriteConflictPolicy
	}
	clean := vfs.CleanPath(p)
	for _, ds := range s.auth.Docsets {
		if ds.WriteConflictPolicy == "" {
			continue
		}
		for _, pm := range ds.Paths {
			display := pm.Display
			if display == "" {
				display = pm.Source
			}
			if pathWithinRoot(vfs.CleanPath(display), clean) {
				if parsed, err := vfs.ParseWriteConflictPolicy(ds.WriteConflictPolicy); err == nil {
					return parsed
				}
				return global
			}
		}
	}
	return global
}

// pathWithinRoot reports whether p is the docset root itself or sits beneath it.
// A root of "/" contains every path.
func pathWithinRoot(root, p string) bool {
	if root == "/" {
		return true
	}
	return p == root || strings.HasPrefix(p, root+"/")
}

func (s *Server) sftpSubsystem(sess ssh.Session) {
	handler := NewSFTPHandler(s.fs)
	server := sftp.NewRequestServer(sess, sftp.Handlers{
		FileGet:  handler,
		FilePut:  handler,
		FileCmd:  handler,
		FileList: handler,
	})
	if err := server.Serve(); err != nil && err.Error() != "EOF" {
		s.logger.Error("sftp server error", "error", err)
	}
}

func (s *Server) shellHandler(next ssh.Handler) ssh.Handler {
	return func(sess ssh.Session) {
		id := s.resolveIdentity(sess)

		fingerprint := "none"
		if id.PublicKey != nil {
			fingerprint = gossh.FingerprintSHA256(id.PublicKey)
		}

		s.logger.Info("session started",
			"remote_addr", id.RemoteAddr,
			"user", id.User,
			"pubkey_fingerprint", fingerprint,
			"session_id", id.SessionID,
		)

		s.metrics.ActiveSessions.Add(1)
		s.metrics.TotalSessions.Add(1)

		if s.onConnect != nil {
			s.onConnect(id)
		}

		defer func() {
			s.metrics.ActiveSessions.Add(-1)
			s.logger.Info("session ended",
				"remote_addr", id.RemoteAddr,
				"user", id.User,
				"session_id", id.SessionID,
			)
			if s.onDisconnect != nil {
				s.onDisconnect(id)
			}
		}()

		// Store the resolved identity on the session context so the shell is
		// built from the same context-carried identity as MCP/HTTP callers —
		// one identity resolution model, one scoping path across transports.
		ctx := contextWithIdentity(sess.Context(), id)
		sh := s.shellForContext(ctx)

		cmd := sess.Command()
		if len(cmd) > 0 {
			cmdLine := joinCommand(cmd)
			s.metrics.TotalCommands.Add(1)
			exitCode := sh.ExecPipeline(cmdLine, sess, sess.Stderr(), sess)
			sess.Exit(exitCode)
			return
		}

		sh.RunInteractive(sess, sess, s.motd, "lore")
		sess.Exit(0)
	}
}

// buildSessionShell constructs a fully-configured shell scoped to the given
// identity: a per-identity filesystem (lore FilteredView, approval gating,
// write isolation, read-tracking CAS), capability-gated allowed actions, and
// OPENLORE_* environment variables. This is the single source of truth for
// per-identity scoping, shared by the SSH shell handler and the MCP/HTTP tool
// handlers so all transports enforce the same access rules.
func (s *Server) buildSessionShell(id Identity) *shell.Shell {
	// Build per-session filesystem filtered by lore (the identity's docsets)
	sessionFS := vfs.FileSystem(s.merge)
	if id.LoreName != "" {
		if docsetNames, ok := s.auth.Lore[id.LoreName]; ok {
			allowed := make(map[string]bool)
			for _, ds := range docsetNames {
				allowed[ds] = true
			}
			sessionFS = s.merge.FilteredView(allowed)
		}
	}
	// Part C approval gating: writes to gated paths become pending requests
	// instead of committing. This wrapper sits *inside* scopedWriteFS so an
	// out-of-scope write is hard-denied before it can become a request.
	if s.authEnforced && s.requests != nil && s.hasApprovalRules() {
		sessionFS = newApprovalFS(sessionFS, s.requests, s.requiresApproval, id.IdentityName, s.bus)
	}
	// canWrite is the single authority gate shared by the FS layer and the
	// action layer: an identity may write only when auth is enforced, it is a
	// named identity (not anonymous), and its token scope grants write. A WIF
	// token narrowed to `read` (or any non-`full` scope) is read-only here even
	// if the underlying identity is write-capable — narrowing wins, fail-closed.
	canWrite := s.authEnforced && id.IdentityName != "" && scopeGrantsWrite(id.Scopes)
	// Part B per-identity write isolation: confine this session's writes to
	// the docset roots it may publish to, so two agents sharing a lore can't
	// write each other's docsets. Anonymous/unrecognized/read-scoped identities
	// get no writable roots (fully read-only at the FS layer). No-op when auth
	// is not enforced.
	if s.authEnforced {
		roots := s.writableDocsetRoots(id)
		if !canWrite {
			roots = nil
		}
		sessionFS = newScopedWriteFS(sessionFS, roots)
	}
	if s.sessionFSFn != nil {
		sessionFS = s.sessionFSFn(id, sessionFS)
	}
	// Session CAS: track the hash of every file read (and written) so a
	// later blind overwrite compare-and-swaps against the version the
	// caller last saw, without naming a hash — an overwrite fails if the
	// file changed since it was read. Outermost so it observes all reads;
	// only meaningful (and only added) when the substrate is writable.
	if !s.config.Readonly {
		if w, ok := sessionFS.(vfs.WritableFS); ok {
			sessionFS = newReadTrackingFS(w)
		}
	}

	sh := shell.NewShell(sessionFS)
	if s.config.DefaultCwd != "" {
		sh.SetCwd(s.config.DefaultCwd)
	}
	// Per-docset / global write-conflict policy for overwrite verbs.
	sh.SetConflictPolicyFn(s.writeConflictPolicy)

	// Capability gating (Part B). Only applies when an auth config is
	// present — without one (local `openlore .` or an embedded KB server
	// that does its own scoping) the shell stays unrestricted. With auth,
	// an unrecognized/anonymous identity is read-only; a recognized
	// identity may write and publish within its docsets.
	if s.authEnforced {
		if canWrite {
			allowed := []cmds.Action{cmds.ActionWrite, cmds.ActionPublish}
			// An identity holding any approval capability may also see and
			// use approve/reject (the exact capability is checked per
			// request when the command runs).
			if len(id.Capabilities) > 0 {
				allowed = append(allowed, cmds.ActionApprove)
			}
			// spawn runs an external command as the OpenLore service
			// user, so it's gated on an explicit `spawn` capability
			// (Part D) — never granted to ordinary writers.
			if hasCapability(id.Capabilities, "spawn") {
				allowed = append(allowed, cmds.ActionSpawn)
			}
			sh.SetAllowedActions(allowed)
		} else {
			sh.SetAllowedActions(nil) // read-only (ActionRead implied)
		}
	}

	// Set identity info as environment variables
	if id.IdentityName != "" {
		sh.SetEnv("OPENLORE_IDENTITY", id.IdentityName)
	}
	// $HOME points at the identity's home docset (enables ~ expansion and
	// `cd` with no arguments).
	if id.HomeDir != "" {
		sh.SetEnv("HOME", id.HomeDir)
	}
	if len(id.Capabilities) > 0 {
		sh.SetEnv("OPENLORE_CAPABILITIES", strings.Join(id.Capabilities, ","))
	}
	// Expose the user as the agent ID so commands like `kb` and
	// the writable VFS can scope operations per-agent.
	if id.User != "" {
		sh.SetEnv("OPENLORE_USER", id.User)
		sh.SetEnv("OPENLORE_AGENT_ID", id.User)
	}
	if id.LoreName != "" {
		sh.SetEnv("OPENLORE_LORE", id.LoreName)
		// Set writable docsets for publish scoping
		if len(id.PublishDocsets) > 0 {
			// Identity has explicit publish scope
			sh.SetEnv("OPENLORE_DOCSETS", strings.Join(id.PublishDocsets, ","))
		} else {
			// Fall back to all docsets in lore
			if docsetNames, ok := s.auth.Lore[id.LoreName]; ok {
				sh.SetEnv("OPENLORE_DOCSETS", strings.Join(docsetNames, ","))
			}
		}
	}

	return sh
}

// anonymousIdentity returns the identity used for callers that present no
// credential (keyless SSH, or an unauthenticated MCP/HTTP request). It mirrors
// the keyless branches of resolveIdentity: the "default" lore when auth is
// enforced, or full access when it is not.
func (s *Server) anonymousIdentity() Identity {
	id := Identity{
		SessionID:   generateSessionID(),
		ConnectedAt: time.Now(),
	}
	if s.authEnforced {
		if _, ok := s.auth.Lore["default"]; ok {
			id.LoreName = "default"
			id.PathAccess = s.resolveLorePathAccess("default")
		}
	} else {
		// Auth not enforced: full access
		id.Scopes = []string{ScopeFull}
		for name := range s.merge.mounts {
			id.PathAccess = append(id.PathAccess, config.PathMapping{
				Source:  "/" + name,
				Display: "/" + name,
			})
		}
	}
	return id
}

// identityCtxKey is the context key under which every transport stores the
// resolved caller Identity. SSH stores it after public-key/cert resolution;
// the MCP/HTTP boundary stores it after bearer-token verification (Phase 1).
type identityCtxKey struct{}

// contextWithIdentity returns ctx carrying the resolved identity. Both the SSH
// shell handler and the MCP/HTTP request boundary call this so that all
// downstream scoping reads the identity from one place, regardless of
// transport.
func contextWithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityCtxKey{}, id)
}

// identityFromContext returns the caller identity previously stored in ctx by
// the transport boundary. If none is present (no credential resolved) it falls
// back to the anonymous identity — mirroring keyless SSH. This is the single
// accessor used by both SSH and MCP/HTTP tool handling.
func (s *Server) identityFromContext(ctx context.Context) Identity {
	if id, ok := ctx.Value(identityCtxKey{}).(Identity); ok {
		return id
	}
	return s.anonymousIdentity()
}

// shellForContext builds a per-identity shell from the identity carried in ctx.
// It is the single factory used by both the SSH shell handler and the MCP/HTTP
// tool handler, so every transport enforces the same per-identity scoping.
func (s *Server) shellForContext(ctx context.Context) *shell.Shell {
	return s.buildSessionShell(s.identityFromContext(ctx))
}

func joinCommand(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += " "
		}
		// Re-quote args that contain shell metacharacters (SSH splits on spaces)
		if strings.ContainsAny(p, " \t") {
			result += "'" + strings.ReplaceAll(p, "'", "'\\''") + "'"
		} else {
			result += p
		}
	}
	return result
}

// ListenAndServe starts the SSH server (blocks).
func (s *Server) ListenAndServe() error {
	opts := []ssh.Option{
		wish.WithAddress(fmt.Sprintf(":%d", s.config.Port)),
		wish.WithHostKeyPath(s.config.HostKeyPath),
		wish.WithSubsystem("sftp", s.sftpSubsystem),
		wish.WithMiddleware(
			s.shellHandler,
			logging.Middleware(),
		),
	}

	if s.config.AllowKeyless {
		opts = append(opts, ssh.EmulatePty())
		// Keyless mode accepts everyone, but we must still capture the
		// client's public key when they present one so identity-based access
		// (lore, publish docsets, whoami) resolves. We therefore set a
		// PublicKeyAuth handler that accepts any key — this makes
		// sess.PublicKey() available for matching in buildIdentity. To still
		// admit clients with *no* key at all (fresh containers, CI runners,
		// brand-new VMs), we additionally enable keyboard-interactive auth
		// that auto-succeeds. Key-bearing clients authenticate via publickey
		// (key captured); keyless clients fall back to keyboard-interactive.
		opts = append(opts, ssh.PublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			return true
		}))
		opts = append(opts, ssh.KeyboardInteractiveAuth(func(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) bool {
			return true
		}))
	} else {
		opts = append(opts, ssh.PublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			if s.config.UnknownIdentity == "deny" && s.authEnforced {
				// Extract underlying key from certificates
				matchKey := key
				var cert *gossh.Certificate
				if c, ok := key.(*gossh.Certificate); ok {
					cert = c
					matchKey = c.Key
				}

				keyStr := string(gossh.MarshalAuthorizedKey(matchKey))
				for _, ident := range s.auth.Identities {
					if ident.PublicKey == keyStr {
						return true
					}
				}

				// Allow cert-authenticated users whose principal matches a lore name
				if cert != nil {
					for _, principal := range cert.ValidPrincipals {
						if _, ok := s.auth.Lore[principal]; ok {
							return true
						}
					}
				}

				return false
			}
			return true
		}))
	}

	if s.config.CAKeysFile != "" {
		opts = append(opts, wish.WithTrustedUserCAKeys(s.config.CAKeysFile))
	}

	srv, err := wish.NewServer(opts...)
	if err != nil {
		return fmt.Errorf("creating SSH server: %w", err)
	}

	if s.config.HostCertFile != "" {
		certBytes, err := os.ReadFile(s.config.HostCertFile)
		if err != nil {
			return fmt.Errorf("reading host certificate: %w", err)
		}
		parsed, _, _, _, err := gossh.ParseAuthorizedKey(certBytes)
		if err != nil {
			return fmt.Errorf("parsing host certificate: %w", err)
		}
		cert, ok := parsed.(*gossh.Certificate)
		if !ok {
			return fmt.Errorf("host certificate file does not contain a certificate")
		}

		hostKeyBytes, err := os.ReadFile(s.config.HostKeyPath)
		if err != nil {
			return fmt.Errorf("reading host key for certificate: %w", err)
		}
		hostSigner, err := gossh.ParsePrivateKey(hostKeyBytes)
		if err != nil {
			return fmt.Errorf("parsing host key for certificate: %w", err)
		}
		certSigner, err := gossh.NewCertSigner(cert, hostSigner)
		if err != nil {
			return fmt.Errorf("creating host certificate signer: %w", err)
		}
		srv.AddHostKey(certSigner)
	}

	s.srv = srv

	if s.config.MetricsPort > 0 {
		metrics.StartServer(s.config.MetricsPort, s.metrics, s.logger)
	}

	if s.config.HTTPPort > 0 {
		webFS := assets.Web()
		if webFS != nil {
			httpCfg := httpserver.Config{
				Port:          s.config.HTTPPort,
				TLSCert:       s.config.TLSCert,
				TLSKey:        s.config.TLSKey,
				HostKeyPath:   s.config.HostKeyPath,
				SSHPort:       s.advertisedSSHPort(),
				Logger:        s.logger,
				ExtraHandlers: map[string]http.Handler{},
			}

			// Third-party license notices, served from the embedded legal
			// filesystem so recipients of a distributed binary/image can read
			// the attributions required by our dependencies' licenses.
			if legalFS := assets.Legal(); legalFS != nil {
				legalHandler := legal.Handler(legalFS)
				httpCfg.ExtraHandlers["/legal"] = legalHandler
				httpCfg.ExtraHandlers["/legal/"] = legalHandler
				s.logger.Info("legal notices mounted", "path", "/legal", "http_port", s.config.HTTPPort)
			}

			// A single MCP server backs both the Streamable HTTP endpoint and
			// the plain JSON HTTP API below. Its `shell` tool builds a
			// per-identity scoped shell from the request context, so MCP/HTTP
			// callers get the same lore/capability scoping as an SSH session.
			var mcpServer *mcp.Server
			if (s.config.MCPEnabled && s.config.MCPPath != "") || (s.config.APIEnabled && s.config.APIPath != "") {
				mcpServer = NewMCPServer(s.fs, withMCPShellFactory(s.shellForContext))
			}

			// Mount the MCP-over-HTTP (Streamable HTTP) endpoint on the HTTP
			// server at the configured path, so it reuses the same port/TLS as
			// the front page (and any load balancer rule fronting it).
			if s.config.MCPEnabled && s.config.MCPPath != "" {
				mcpPath := "/" + strings.Trim(s.config.MCPPath, "/")
				mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
					return mcpServer
				}, nil)
				// Posture-aware bearer auth (§4): identity from a verified token
				// (or anonymous) is placed on the request context, which the
				// Streamable transport carries into the tool handler.
				h := s.authMiddleware(mcpHandler)
				httpCfg.ExtraHandlers[mcpPath] = h
				httpCfg.ExtraHandlers[mcpPath+"/"] = h
				s.logger.Info("MCP endpoint mounted", "path", mcpPath, "http_port", s.config.HTTPPort)
			}

			// Mount the plain JSON HTTP API (backed by the same MCP server) at
			// the configured path. It exposes the MCP tools over simple REST
			// endpoints: POST {path}/shell and GET {path}/commands.
			if s.config.APIEnabled && s.config.APIPath != "" {
				apiPath := "/" + strings.Trim(s.config.APIPath, "/")
				api := NewMCPHTTPAPI(mcpServer)
				httpCfg.ExtraHandlers[apiPath+"/"] = s.authMiddleware(api.Handler(apiPath))
				s.logger.Info("HTTP API mounted", "path", apiPath, "http_port", s.config.HTTPPort)
			}

			// Mount the OAuth token endpoint + JWKS when token auth is enabled
			// (auth.tokens configured). This is the single mint step for login
			// (authorization_code) and, later, WIF exchange (jwt-bearer).
			if s.tokens != nil {
				for path, h := range s.oauthRoutes() {
					httpCfg.ExtraHandlers[path] = h
				}
				s.logger.Info("token endpoint mounted", "path", tokenPath, "http_port", s.config.HTTPPort)
				s.logger.Info("authorize endpoint mounted", "path", authorizePath, "http_port", s.config.HTTPPort)
				s.logger.Info("client registration mounted", "path", registrationPath, "http_port", s.config.HTTPPort)
				s.logger.Info("oauth metadata mounted", "path", protectedResourceMetadataPath, "http_port", s.config.HTTPPort)
			}

			if s.passkeys != nil {
				// Build the HTTP base URL for the passkey shell command.
				// If rp_origins are configured, use the first one as the base URL
				// (handles TLS termination at a load balancer).
				var baseURL string
				if len(s.config.Passkeys.RPOrigins) > 0 {
					baseURL = strings.TrimRight(s.config.Passkeys.RPOrigins[0], "/")
				} else {
					scheme := "http"
					if s.config.TLSCert != "" {
						scheme = "https"
					}
					baseURL = fmt.Sprintf("%s://localhost:%d", scheme, s.config.HTTPPort)
					if s.config.Passkeys.RPID != "" && s.config.Passkeys.RPID != "localhost" {
						baseURL = fmt.Sprintf("%s://%s", scheme, s.config.Passkeys.RPID)
						if (scheme == "http" && s.config.HTTPPort != 80) || (scheme == "https" && s.config.HTTPPort != 443) {
							baseURL = fmt.Sprintf("%s:%d", baseURL, s.config.HTTPPort)
						}
					}
				}

				passkeys.RegisterShellCommand(s.passkeys, baseURL)

				// Wire the token-minting seam so passkey login can drive the
				// OAuth authorization-code flow and resolve identity → lore for
				// the browser cookie. Always set (not gated on token auth): the
				// Server implements the full interface, and IssueAuthCode /
				// CompleteAuthorize return ok=false when token auth is disabled.
				s.passkeys.SetTokenIssuer(s)

				httpCfg.Extenders = append(httpCfg.Extenders, s.passkeys)

				lorePath := s.config.Passkeys.LorePath
				if lorePath == "" {
					lorePath = "/lore"
				}
				lorePath = "/" + strings.Trim(lorePath, "/")
				httpCfg.ExtraHandlers[lorePath+"/"] = s.passkeys.LoreBrowserHandler(s.fs)

				cmds.PublishBaseURL = baseURL + lorePath
			}

			httpSrv := httpserver.New(webFS, httpCfg)
			httpSrv.Start()
			s.httpSrv = httpSrv
		}
	}

	s.logger.Info("SSH server starting", "port", s.config.Port)
	return s.srv.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.passkeys != nil {
		s.passkeys.Shutdown()
	}
	// Give in-flight async jobs (Part D) a few seconds to commit their
	// write-back before we exit, shrinking the loss window.
	if s.jobs != nil {
		if !s.jobs.Drain(jobDrainTimeout) {
			s.logger.Warn("shutdown: async jobs still running after drain timeout", "timeout", jobDrainTimeout)
		}
	}
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}
