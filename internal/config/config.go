package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/aakarim/go-openlore/pkg/vfs"
	"gopkg.in/yaml.v3"
)

// Config holds the resolved server configuration.
type Config struct {
	ConfigVersion   string
	Port            int
	MetricsPort     int
	HostKeyPath     string
	AllowKeyless    bool
	UnknownIdentity string // "allow" (default) or "deny"
	DefaultCwd      string
	MOTD            string
	AuthFile        string
	SkillsDir       string
	// DataDir is the server's writable control-plane data root. Distinct from
	// docset content. Defaults to ./.openlore.
	DataDir         string
	HTTPPort        int
	ExternalSSHPort int // advertised SSH port (for X-SSH-Port header behind a LB)
	// MCPEnabled controls whether the always-on MCP-over-HTTP endpoint runs.
	// Default true. The endpoint is mounted at MCPPath on the HTTP server.
	MCPEnabled bool
	MCPPath    string
	// APIEnabled controls whether the plain JSON HTTP API (backed by the MCP
	// server) runs. Default true. It is mounted at APIPath on the HTTP server.
	APIEnabled   bool
	APIPath      string
	TLSCert      string
	TLSKey       string
	CAKeysFile   string
	HostCertFile string
	Files        FilesConfig
	Folders      []FolderConfig
	Passkeys     PasskeysConfig
	// Shellexec is the external-command middleware config (pre_read, pre_commit,
	// post_write) run by the built-in shellexec plugin. Replaces the legacy
	// event-bus `hooks` path with middleware on the read/write chains.
	Shellexec ShellexecConfig
	Logger    *slog.Logger

	// Readonly is the global write lock. Default true: the substrate is a
	// read-only filesystem and no write verbs are available. Set false to
	// enable the experimental writable substrate (SetWriteable is called at
	// startup). Global readonly is a hard physical lock — a per-docset
	// readonly=false cannot loosen it.
	Readonly bool

	// WriteConflictPolicy is the global default policy for whole-file overwrite
	// verbs (`>`, tee, sed -i, publish). Default "hash" (compare-and-swap); set
	// "last_write_wins" for unconditional overwrites. A per-docset override
	// (DocsetSpec.WriteConflictPolicy) takes precedence for that docset.
	WriteConflictPolicy vfs.WriteConflictPolicy

	// MaxJobs bounds concurrent async `spawn` jobs (Part D). Default 8.
	MaxJobs int

	// Tokens configures bearer-token issuance/verification for the MCP + HTTP
	// API. This is server infrastructure (issuer identity, audience, signing
	// key, TTLs) — not per-lore access policy — so it lives in openlore.yml
	// alongside passkeys, not in lore.json. When nil, token auth is disabled
	// and the MCP/HTTP endpoints behave as anonymous callers (Phase 0).
	Tokens *AuthTokensConfig

	// OIDCIssuers are external IdPs whose JWTs may be exchanged for OpenLore
	// tokens at the token endpoint via the jwt-bearer grant (workload identity
	// federation). When set, each issuer's JWKS is fetched (discovery) and its
	// assertions are verified and mapped to identities. Server infrastructure,
	// hence openlore.yml.
	OIDCIssuers []OIDCIssuer

	// Track sources for conflict detection.
	configFileLoaded   bool
	embeddedConfigUsed bool
}

// ShellexecConfig is the openlore.yml `shellexec:` block: external commands run
// as middleware on the read and write paths. pre_read runs before a read (may
// abort it), pre_commit runs before a write commits (may reject it), post_write
// runs after a durable commit (fire-and-forget: never halts the log).
type ShellexecConfig struct {
	PreRead   []ShellexecCmd `yaml:"pre_read"`
	PreCommit []ShellexecCmd `yaml:"pre_commit"`
	PostWrite []ShellexecCmd `yaml:"post_write"`
}

// ShellexecCmd is a single external command run by the shellexec plugin. It is
// run via `sh -c` with the OPENLORE_* env protocol.
type ShellexecCmd struct {
	// Cmd is the shell command line to execute.
	Cmd string `yaml:"cmd"`
	// Timeout is a duration string (e.g. "30s") capping wall-clock runtime.
	// Empty means 30s. A timeout counts as a failure.
	Timeout string `yaml:"timeout"`
	// FailOnError makes a non-zero exit fatal to the operation for pre_read /
	// pre_commit (the read/write is aborted). Defaults to true (nil → true).
	// Ignored for post_write, which never halts the log.
	FailOnError *bool `yaml:"fail_on_error"`
	// Debounce is a duration string coalescing repeated pre_read hits on the
	// same path. Empty means 2s. Only applies to pre_read.
	Debounce string `yaml:"debounce"`
	// Async runs the command in the background (fire-and-forget). Default false
	// (synchronous). An async pre_read / pre_commit cannot abort the operation.
	Async bool `yaml:"async"`
}

// IsEmpty reports whether no shellexec commands are configured.
func (c ShellexecConfig) IsEmpty() bool {
	return len(c.PreRead) == 0 && len(c.PreCommit) == 0 && len(c.PostWrite) == 0
}

// OKFDocsetConfig configures the built-in Open Knowledge Format validator for a
// docset. Its presence on a DocsetSpec activates OKF validation across that
// docset's subtree (defaults: enforce=true, patterns=["*.md"]).
//
// It lives on the docset (in lore.json) rather than as a global block so OKF
// scoping is defined in the same place as the docset's paths and grants and can
// never drift from them: a write is validated by the OKF config of the docset
// that owns its path (the longest matching display root, exactly as authz
// resolves grants). Include/exclude for narrower subtrees is expressed with
// nested docsets — a child docset with OKF adds validation to that subtree; a
// child docset without OKF shadows a parent's OKF and exempts that subtree.
type OKFDocsetConfig struct {
	// Enforce rejects non-conformant writes when true (nil → true, the default).
	// When false, a non-conformant write is logged but allowed through.
	Enforce *bool `json:"enforce,omitempty"`
	// Patterns are globs matched against a write target's basename to select
	// which files are validated. Empty defaults to ["*.md"].
	Patterns []string `json:"patterns,omitempty"`
}

// PasskeysConfig holds WebAuthn passkey configuration.
type PasskeysConfig struct {
	Enabled      bool
	RPID         string
	RPName       string
	RPOrigins    []string
	LorePath     string
	PasskeysFile string
	SessionTTL   string // parsed as time.Duration
}

// FilesConfig controls which files are served.
type FilesConfig struct {
	Allowed []string
	Denied  []string
	Ignore  []string
}

// FolderConfig defines an additional named folder mount.
type FolderConfig struct {
	Name string
	Path string
}

// AuthConfig is loaded from lore.json.
type AuthConfig struct {
	AllowKeyless    *bool                 `json:"allow_keyless,omitempty"`
	UnknownIdentity string                `json:"unknown_identity,omitempty"`
	DefaultCwd      string                `json:"default_cwd,omitempty"`
	Docsets         map[string]DocsetSpec `json:"docsets"`
	// Default is the docset→grant map applied to keyless / unrecognized callers
	// (the former `lore.default`). Empty = no access for anonymous sessions.
	Default    map[string]string `json:"default,omitempty"`
	Identities []AuthIdentity    `json:"identities"`
}

// AuthTokensConfig controls the bearer-token issuer for the MCP + HTTP API.
// It is server infrastructure and loaded from openlore.yml (hence yaml tags).
type AuthTokensConfig struct {
	Issuer     string `yaml:"issuer" json:"issuer,omitempty"`           // `iss` claim + JWKS base
	Audience   string `yaml:"audience" json:"audience,omitempty"`       // required `aud`; one per instance
	AccessTTL  string `yaml:"access_ttl" json:"access_ttl,omitempty"`   // duration string, default 30m
	RefreshTTL string `yaml:"refresh_ttl" json:"refresh_ttl,omitempty"` // duration string, default 720h
}

// OIDCIssuer is an external IdP trusted for WIF token exchange. Server
// infrastructure, loaded from openlore.yml.
type OIDCIssuer struct {
	IssuerURL string   `yaml:"issuer_url" json:"issuer_url"`
	JWKS      JWKSSpec `yaml:"jwks" json:"jwks,omitempty"`
}

// JWKSSpec configures how an OIDC issuer's public keys are obtained. Only
// "discovery" (fetch from the issuer's .well-known/openid-configuration) is
// supported today; empty defaults to discovery.
type JWKSSpec struct {
	Mode string `yaml:"mode" json:"mode,omitempty"` // "discovery" (default)
}

// DocsetSpec defines a named set of path mappings.
type DocsetSpec struct {
	Paths []PathMapping `json:"paths"`
	// Aliases are alternate display roots for the first path. They expose the
	// same content while the first path remains canonical for home, inbox,
	// policy, hooks, and changesets.
	Aliases []string `json:"aliases,omitempty"`
	// Inbox names a subfolder (VFS path, relative to a docset root or absolute)
	// that the `publish` grant confines create/edit to. Empty = the docset has
	// no inbox, so a `publish` grant on it can write nothing.
	Inbox string `json:"inbox,omitempty"`
	// MaxWriteSize caps a single write's bytes for this docset; 0 = default (2.5MB).
	MaxWriteSize int64 `json:"max_write_size,omitempty"`

	// Readonly is the per-docset policy check (enforced in the write pipeline,
	// not on the substrate). nil means "inherit" (writable when the global lock
	// is open). A docset can only further restrict: setting it true blocks
	// writes to this docset even when the global lock is open; setting it false
	// is meaningless when the global lock is closed.
	Readonly *bool `json:"readonly,omitempty"`

	// WriteConflictPolicy overrides the global write-conflict policy for writes
	// to this docset. "" inherits Config.WriteConflictPolicy; "hash" forces
	// compare-and-swap overwrites; "last_write_wins" forces unconditional ones.
	WriteConflictPolicy string `json:"write_conflict_policy,omitempty"`

	// OKF, when non-nil, activates the built-in Open Knowledge Format validator
	// for this docset's subtree (see OKFDocsetConfig). nil means OKF is off for
	// this docset; scope narrower subtrees with nested docsets.
	OKF *OKFDocsetConfig `json:"okf,omitempty"`
}

// PathMapping represents a path entry — either a simple string path or a
// source→display mapping.
type PathMapping struct {
	Source  string // the real path (relative to root dir or assets/lore)
	Display string // the path shown in the shell (empty = same as Source)
}

// UnmarshalJSON supports both string and {"source": "display"} forms.
func (p *PathMapping) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		p.Source = s
		p.Display = s
		return nil
	}

	// Try dict (single key-value)
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("path must be a string or {\"source\": \"display\"} object: %s", string(data))
	}
	if len(m) != 1 {
		return fmt.Errorf("path object must have exactly one key: %s", string(data))
	}
	for k, v := range m {
		p.Source = k
		p.Display = v
	}
	return nil
}

// AuthIdentity defines a user identity and its per-docset grants.
type AuthIdentity struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
	// PublicKey is optional: an identity may exist purely as a passkey/token
	// login target (no SSH key). Empty = no SSH public-key auth for this identity.
	PublicKey string `json:"public_key,omitempty"`
	// Docsets maps a docset name to the grant this identity holds on it
	// (e.g. "ro", "rw", "publish"). Grant names must be registered grant types
	// at server startup, else the server refuses to boot (fail-closed).
	Docsets map[string]string `json:"docsets"`
	// Home names the docset that serves as this identity's home directory. Its
	// display path becomes $HOME and the session's initial working directory.
	// Must be one of the docsets in the identity's grants. Empty = no home.
	Home string `json:"home,omitempty"`
	// Capabilities are the extra capabilities this identity holds, e.g.
	// "spawn" to authorize the async external-work command.
	Capabilities []string `json:"capabilities,omitempty"`

	// Match lists the token-claim predicates that resolve TO this identity.
	// Resolution criteria live on the identity they select (rather than a
	// separate rule list) since every rule maps to exactly one identity. The
	// human case needs no entry: a token whose `sub` equals this identity's
	// Name resolves here implicitly. WIF exchanges (jwt-bearer) match on
	// `sub`/`sub_prefix`/`aud`/`claims` entries with narrowing `scope`/`ttl`.
	Match []IdentityMatch `json:"match,omitempty"`
}

// IdentityMatch is a token-claim predicate attached to an AuthIdentity. When a
// verified assertion's claims satisfy it (all specified fields must hold), the
// assertion resolves to the enclosing identity. Exact `sub` takes precedence
// over `sub_prefix`/`aud`/`claims` pattern matches; `scope` narrows and `ttl`
// caps the brokered OpenLore token.
type IdentityMatch struct {
	Sub       string            `json:"sub,omitempty"`
	SubPrefix string            `json:"sub_prefix,omitempty"`
	Aud       string            `json:"aud,omitempty"`
	Claims    map[string]string `json:"claims,omitempty"`
	Scope     string            `json:"scope,omitempty"` // narrowing scope for matched tokens (WIF)
	TTL       string            `json:"ttl,omitempty"`   // caps brokered token TTL (WIF)
}

// Option is a functional option for configuring the server.
type Option func(*Config) error

// fileConfig mirrors Config for YAML deserialization.
type fileConfig struct {
	ConfigVersion       string           `yaml:"version"`
	Port                int              `yaml:"port"`
	MetricsPort         int              `yaml:"metrics_port"`
	HostKeyPath         string           `yaml:"host_key_path"`
	MOTD                string           `yaml:"motd"`
	MOTDFile            string           `yaml:"motd_file"`
	AuthFile            string           `yaml:"auth_file"`
	SkillsDir           string           `yaml:"skills_dir"`
	DataDir             string           `yaml:"data_dir"`
	HTTPPort            int              `yaml:"http_port"`
	ExternalSSHPort     int              `yaml:"external_ssh_port"`
	MCP                 *mcpYAML         `yaml:"mcp"`
	API                 *apiYAML         `yaml:"api"`
	TLSCert             string           `yaml:"tls_cert"`
	TLSKey              string           `yaml:"tls_key"`
	CAKeysFile          string           `yaml:"ca_keys_file"`
	HostCertFile        string           `yaml:"host_cert_file"`
	DefaultCwd          string           `yaml:"default_cwd"`
	Files               *filesYAML       `yaml:"files"`
	Folders             []FolderConfig   `yaml:"folders"`
	Passkeys            *passkeysYAML    `yaml:"passkeys"`
	Shellexec           *ShellexecConfig `yaml:"shellexec"`
	Readonly            *bool            `yaml:"readonly"`
	WriteConflictPolicy string           `yaml:"write_conflict_policy"`
	MaxJobs             int              `yaml:"max_jobs"`
	// Tokens + OIDCIssuers are server infrastructure (bearer-token issuance for
	// the MCP + HTTP API), hence configured here rather than in lore.json.
	Tokens      *AuthTokensConfig `yaml:"tokens"`
	OIDCIssuers []OIDCIssuer      `yaml:"oidc_issuers"`
}

type mcpYAML struct {
	Enabled *bool  `yaml:"enabled"`
	Path    string `yaml:"path"`
}

type apiYAML struct {
	Enabled *bool  `yaml:"enabled"`
	Path    string `yaml:"path"`
}

type passkeysYAML struct {
	Enabled      bool     `yaml:"enabled"`
	RPID         string   `yaml:"rp_id"`
	RPName       string   `yaml:"rp_name"`
	RPOrigins    []string `yaml:"rp_origins"`
	LorePath     string   `yaml:"lore_path"`
	PasskeysFile string   `yaml:"passkeys_file"`
	SessionTTL   string   `yaml:"session_ttl"`
}

type filesYAML struct {
	Allowed []string `yaml:"allowed"`
	Denied  []string `yaml:"denied"`
	Ignore  []string `yaml:"ignore"`
}

// New creates a Config by applying options to the defaults.
// Returns an error if both a config file and embedded config are used.
func New(opts ...Option) (Config, error) {
	cfg := Config{
		Port:                2222,
		HTTPPort:            8080,
		MetricsPort:         3000,
		MCPEnabled:          true,
		MCPPath:             "/mcp",
		APIEnabled:          true,
		APIPath:             "/api",
		HostKeyPath:         ".ssh/openlore_ed25519",
		AllowKeyless:        true,
		UnknownIdentity:     "allow",
		DefaultCwd:          "/openlore",
		Readonly:            true,                           // safe default: read-only substrate
		WriteConflictPolicy: vfs.DefaultWriteConflictPolicy, // "hash": overwrites are compare-and-swap
		MaxJobs:             8,                              // bound concurrent async spawn jobs
		Files: FilesConfig{
			Allowed: []string{
				"*.md", "*.markdown", "*.txt",
				"*.html", "*.htm", "*.css", "*.js",
				"*.json", "*.yaml", "*.yml",
				"*.csv", "*.tsv", "*.xml", "*.toml",
				"*.png", "*.jpg", "*.jpeg", "*.gif", "*.svg", "*.webp",
			},
			Ignore: []string{
				".git/**", "node_modules/**", ".env*", "**/.DS_Store",
			},
		},
	}

	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return Config{}, err
		}
	}

	if cfg.configFileLoaded && cfg.embeddedConfigUsed {
		return Config{}, errors.New("conflict: cannot use both a config file and embedded config")
	}

	return cfg, nil
}

// WithConfigFile loads configuration from a YAML file.
// Fields in the file override defaults. If the file does not exist,
// no error is returned and the config is unchanged.
func WithConfigFile(path string) Option {
	return func(cfg *Config) error {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("reading config file: %w", err)
		}

		var fc fileConfig
		if err := yaml.Unmarshal(data, &fc); err != nil {
			return fmt.Errorf("parsing config file: %w", err)
		}

		cfg.configFileLoaded = true

		if fc.ConfigVersion != "" {
			cfg.ConfigVersion = fc.ConfigVersion
		}
		if fc.Port != 0 {
			cfg.Port = fc.Port
		}
		if fc.MetricsPort != 0 {
			cfg.MetricsPort = fc.MetricsPort
		}
		if fc.HostKeyPath != "" {
			cfg.HostKeyPath = fc.HostKeyPath
		}
		if fc.MOTDFile != "" {
			motdData, err := os.ReadFile(fc.MOTDFile)
			if err != nil {
				return fmt.Errorf("reading MOTD file %q: %w", fc.MOTDFile, err)
			}
			cfg.MOTD = string(motdData)
		} else if fc.MOTD != "" {
			cfg.MOTD = fc.MOTD
		}
		if fc.AuthFile != "" {
			cfg.AuthFile = fc.AuthFile
		}
		if fc.SkillsDir != "" {
			cfg.SkillsDir = fc.SkillsDir
		}
		if fc.DataDir != "" {
			cfg.DataDir = fc.DataDir
		}
		if fc.HTTPPort != 0 {
			cfg.HTTPPort = fc.HTTPPort
		}
		if fc.ExternalSSHPort != 0 {
			cfg.ExternalSSHPort = fc.ExternalSSHPort
		}
		if fc.TLSCert != "" {
			cfg.TLSCert = fc.TLSCert
		}
		if fc.TLSKey != "" {
			cfg.TLSKey = fc.TLSKey
		}
		if fc.CAKeysFile != "" {
			cfg.CAKeysFile = fc.CAKeysFile
		}
		if fc.HostCertFile != "" {
			cfg.HostCertFile = fc.HostCertFile
		}
		if fc.DefaultCwd != "" {
			cfg.DefaultCwd = fc.DefaultCwd
		}
		if fc.Files != nil {
			if len(fc.Files.Allowed) > 0 {
				cfg.Files.Allowed = fc.Files.Allowed
			}
			if len(fc.Files.Denied) > 0 {
				cfg.Files.Denied = fc.Files.Denied
			}
			if len(fc.Files.Ignore) > 0 {
				cfg.Files.Ignore = fc.Files.Ignore
			}
		}
		if len(fc.Folders) > 0 {
			cfg.Folders = fc.Folders
		}
		if fc.Shellexec != nil {
			cfg.Shellexec = *fc.Shellexec
		}
		if fc.Readonly != nil {
			cfg.Readonly = *fc.Readonly
		}
		if fc.WriteConflictPolicy != "" {
			p, err := vfs.ParseWriteConflictPolicy(fc.WriteConflictPolicy)
			if err != nil {
				return err
			}
			cfg.WriteConflictPolicy = p
		}
		if fc.MaxJobs > 0 {
			cfg.MaxJobs = fc.MaxJobs
		}
		applyPasskeysConfig(cfg, fc.Passkeys)
		applyMCPConfig(cfg, fc.MCP)
		applyAPIConfig(cfg, fc.API)
		applyTokensConfig(cfg, fc.Tokens, fc.OIDCIssuers)

		return nil
	}
}

// applyTokensConfig maps bearer-token server settings from the file config.
func applyTokensConfig(cfg *Config, tokens *AuthTokensConfig, oidc []OIDCIssuer) {
	if tokens != nil {
		cfg.Tokens = tokens
	}
	if len(oidc) > 0 {
		cfg.OIDCIssuers = oidc
	}
}

// WithEmbeddedConfig loads config from an embedded YAML byte slice.
// The MOTD fallback is set separately from the config fields.
func WithEmbeddedConfig(data []byte, motdFallback string) Option {
	return func(cfg *Config) error {
		if len(data) > 0 {
			cfg.embeddedConfigUsed = true

			var fc fileConfig
			if err := yaml.Unmarshal(data, &fc); err != nil {
				return fmt.Errorf("parsing embedded config: %w", err)
			}

			if fc.ConfigVersion != "" {
				cfg.ConfigVersion = fc.ConfigVersion
			}
			if fc.Port != 0 {
				cfg.Port = fc.Port
			}
			if fc.MetricsPort != 0 {
				cfg.MetricsPort = fc.MetricsPort
			}
			if fc.HostKeyPath != "" {
				cfg.HostKeyPath = fc.HostKeyPath
			}
			if fc.MOTDFile != "" {
				motdData, err := os.ReadFile(fc.MOTDFile)
				if err != nil {
					return fmt.Errorf("reading MOTD file %q: %w", fc.MOTDFile, err)
				}
				cfg.MOTD = string(motdData)
			} else if fc.MOTD != "" {
				cfg.MOTD = fc.MOTD
			}
			if fc.AuthFile != "" {
				cfg.AuthFile = fc.AuthFile
			}
			if fc.SkillsDir != "" {
				cfg.SkillsDir = fc.SkillsDir
			}
			if fc.DataDir != "" {
				cfg.DataDir = fc.DataDir
			}
			if fc.HTTPPort != 0 {
				cfg.HTTPPort = fc.HTTPPort
			}
			if fc.ExternalSSHPort != 0 {
				cfg.ExternalSSHPort = fc.ExternalSSHPort
			}
			if fc.TLSCert != "" {
				cfg.TLSCert = fc.TLSCert
			}
			if fc.TLSKey != "" {
				cfg.TLSKey = fc.TLSKey
			}
			if fc.CAKeysFile != "" {
				cfg.CAKeysFile = fc.CAKeysFile
			}
			if fc.HostCertFile != "" {
				cfg.HostCertFile = fc.HostCertFile
			}
			if fc.DefaultCwd != "" {
				cfg.DefaultCwd = fc.DefaultCwd
			}
			if fc.Files != nil {
				if len(fc.Files.Allowed) > 0 {
					cfg.Files.Allowed = fc.Files.Allowed
				}
				if len(fc.Files.Denied) > 0 {
					cfg.Files.Denied = fc.Files.Denied
				}
				if len(fc.Files.Ignore) > 0 {
					cfg.Files.Ignore = fc.Files.Ignore
				}
			}
			if len(fc.Folders) > 0 {
				cfg.Folders = fc.Folders
			}
			if fc.Shellexec != nil {
				cfg.Shellexec = *fc.Shellexec
			}
			if fc.Readonly != nil {
				cfg.Readonly = *fc.Readonly
			}
			if fc.WriteConflictPolicy != "" {
				p, err := vfs.ParseWriteConflictPolicy(fc.WriteConflictPolicy)
				if err != nil {
					return err
				}
				cfg.WriteConflictPolicy = p
			}
			if fc.MaxJobs > 0 {
				cfg.MaxJobs = fc.MaxJobs
			}
			applyPasskeysConfig(cfg, fc.Passkeys)
			applyMCPConfig(cfg, fc.MCP)
			applyAPIConfig(cfg, fc.API)
			applyTokensConfig(cfg, fc.Tokens, fc.OIDCIssuers)
		}

		// MOTD fallback: only set if nothing else has set it yet
		if cfg.MOTD == "" && motdFallback != "" {
			cfg.MOTD = motdFallback
		}

		return nil
	}
}

// WithPort sets the SSH server port.
func WithPort(port int) Option {
	return func(cfg *Config) error {
		cfg.Port = port
		return nil
	}
}

// WithMetricsPort sets the metrics HTTP port. 0 disables metrics.
func WithMetricsPort(port int) Option {
	return func(cfg *Config) error {
		cfg.MetricsPort = port
		return nil
	}
}

// WithReadonly sets the global write lock. true (the default) keeps the
// substrate read-only; false enables the experimental writable substrate.
func WithReadonly(readonly bool) Option {
	return func(cfg *Config) error {
		cfg.Readonly = readonly
		return nil
	}
}

// WithWriteConflictPolicy sets the global default write-conflict policy for
// whole-file overwrite verbs. Empty resolves to the default (hash). Invalid
// values are rejected.
func WithWriteConflictPolicy(policy string) Option {
	return func(cfg *Config) error {
		p, err := vfs.ParseWriteConflictPolicy(policy)
		if err != nil {
			return err
		}
		cfg.WriteConflictPolicy = p
		return nil
	}
}

// WithHostKeyPath sets the path to the SSH host key.
func WithHostKeyPath(path string) Option {
	return func(cfg *Config) error {
		cfg.HostKeyPath = path
		return nil
	}
}

// WithAllowKeyless controls whether keyless SSH connections are allowed.
func WithAllowKeyless(allow bool) Option {
	return func(cfg *Config) error {
		cfg.AllowKeyless = allow
		return nil
	}
}

// WithDefaultCwd sets the default working directory for shell sessions.
func WithDefaultCwd(cwd string) Option {
	return func(cfg *Config) error {
		cfg.DefaultCwd = cwd
		return nil
	}
}

// WithMOTD sets the message of the day, replacing any previous value.
func WithMOTD(motd string) Option {
	return func(cfg *Config) error {
		cfg.MOTD = motd
		return nil
	}
}

// WithMOTDFile loads the MOTD from a file path, replacing any previous value.
func WithMOTDFile(path string) Option {
	return func(cfg *Config) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading MOTD file: %w", err)
		}
		cfg.MOTD = string(data)
		return nil
	}
}

// WithAuthFile sets the path to the auth.json file.
func WithAuthFile(path string) Option {
	return func(cfg *Config) error {
		cfg.AuthFile = path
		return nil
	}
}

// WithSkillsDir sets the directory for loading runtime skills.
func WithSkillsDir(dir string) Option {
	return func(cfg *Config) error {
		cfg.SkillsDir = dir
		return nil
	}
}

// WithDataDir sets the server's writable control-plane data root.
func WithDataDir(dir string) Option {
	return func(cfg *Config) error {
		cfg.DataDir = dir
		return nil
	}
}

// WithAllowedPatterns sets the file patterns to serve.
func WithAllowedPatterns(patterns []string) Option {
	return func(cfg *Config) error {
		cfg.Files.Allowed = patterns
		return nil
	}
}

// WithIgnorePatterns sets the ignore patterns.
func WithIgnorePatterns(patterns []string) Option {
	return func(cfg *Config) error {
		cfg.Files.Ignore = patterns
		return nil
	}
}

// WithLogger sets the structured logger.
func WithLogger(logger *slog.Logger) Option {
	return func(cfg *Config) error {
		cfg.Logger = logger
		return nil
	}
}

// WithHTTPPort sets the HTTP front page server port. 0 disables it.
func WithHTTPPort(port int) Option {
	return func(cfg *Config) error {
		cfg.HTTPPort = port
		return nil
	}
}

// WithCAKeysFile sets the path to a file containing trusted CA public keys
// for SSH certificate authentication (analogous to OpenSSH TrustedUserCAKeys).
func WithCAKeysFile(path string) Option {
	return func(cfg *Config) error {
		cfg.CAKeysFile = path
		return nil
	}
}

// WithHostCertFile sets the path to the SSH host certificate file
// (signed by a CA, analogous to OpenSSH HostCertificate).
func WithHostCertFile(path string) Option {
	return func(cfg *Config) error {
		cfg.HostCertFile = path
		return nil
	}
}

// WithTLS sets TLS certificate and key paths for the HTTP server.
func WithTLS(cert, key string) Option {
	return func(cfg *Config) error {
		cfg.TLSCert = cert
		cfg.TLSKey = key
		return nil
	}
}

// applyMCPConfig merges an mcpYAML into the config.
func applyMCPConfig(cfg *Config, m *mcpYAML) {
	if m == nil {
		return
	}
	if m.Enabled != nil {
		cfg.MCPEnabled = *m.Enabled
	}
	if m.Path != "" {
		cfg.MCPPath = m.Path
	}
}

// WithMCPPath sets the path the MCP-over-HTTP endpoint is mounted at on the
// HTTP server (e.g. "/mcp").
func WithMCPPath(path string) Option {
	return func(cfg *Config) error {
		cfg.MCPPath = path
		return nil
	}
}

// WithMCPEnabled toggles the MCP-over-HTTP endpoint.
func WithMCPEnabled(enabled bool) Option {
	return func(cfg *Config) error {
		cfg.MCPEnabled = enabled
		return nil
	}
}

// applyAPIConfig merges an apiYAML into the config.
func applyAPIConfig(cfg *Config, a *apiYAML) {
	if a == nil {
		return
	}
	if a.Enabled != nil {
		cfg.APIEnabled = *a.Enabled
	}
	if a.Path != "" {
		cfg.APIPath = a.Path
	}
}

// WithAPIPath sets the path the JSON HTTP API is mounted at on the HTTP server
// (e.g. "/api").
func WithAPIPath(path string) Option {
	return func(cfg *Config) error {
		cfg.APIPath = path
		return nil
	}
}

// WithAPIEnabled toggles the JSON HTTP API.
func WithAPIEnabled(enabled bool) Option {
	return func(cfg *Config) error {
		cfg.APIEnabled = enabled
		return nil
	}
}

// applyPasskeysConfig merges a passkeysYAML into the config.
func applyPasskeysConfig(cfg *Config, pk *passkeysYAML) {
	if pk == nil {
		return
	}
	cfg.Passkeys.Enabled = pk.Enabled
	if pk.RPID != "" {
		cfg.Passkeys.RPID = pk.RPID
	}
	if pk.RPName != "" {
		cfg.Passkeys.RPName = pk.RPName
	}
	if len(pk.RPOrigins) > 0 {
		cfg.Passkeys.RPOrigins = pk.RPOrigins
	}
	if pk.LorePath != "" {
		cfg.Passkeys.LorePath = pk.LorePath
	}
	if pk.PasskeysFile != "" {
		cfg.Passkeys.PasskeysFile = pk.PasskeysFile
	}
	if pk.SessionTTL != "" {
		cfg.Passkeys.SessionTTL = pk.SessionTTL
	}
}

// WithPasskeys sets the passkeys configuration.
func WithPasskeys(pk PasskeysConfig) Option {
	return func(cfg *Config) error {
		cfg.Passkeys = pk
		return nil
	}
}

// LoadAuthConfig loads auth configuration from a JSON file.
func LoadAuthConfig(path string) (*AuthConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var auth AuthConfig
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, err
	}

	// Every docset referenced by an identity's grants or by the anonymous
	// default must exist; home must be one of the identity's granted docsets.
	// Grant *names* are validated against the registered grant types at server
	// startup (fail-closed), not here — config parsing does not know the plugin
	// set.
	for name, grant := range auth.Default {
		if grant == "" {
			return nil, fmt.Errorf("default grant for docset %q is empty", name)
		}
		if _, ok := auth.Docsets[name]; !ok {
			return nil, fmt.Errorf("default references unknown docset %q", name)
		}
	}
	for _, ident := range auth.Identities {
		for name, grant := range ident.Docsets {
			if grant == "" {
				return nil, fmt.Errorf("identity %q grant for docset %q is empty", ident.Name, name)
			}
			if _, ok := auth.Docsets[name]; !ok {
				return nil, fmt.Errorf("identity %q references unknown docset %q", ident.Name, name)
			}
		}
		if ident.Home != "" {
			if _, ok := ident.Docsets[ident.Home]; !ok {
				return nil, fmt.Errorf("identity %q home docset %q is not in its grants", ident.Name, ident.Home)
			}
		}
	}

	return &auth, nil
}
