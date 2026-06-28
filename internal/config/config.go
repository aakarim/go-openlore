package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"

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
	HTTPPort        int
	ExternalSSHPort int // advertised SSH port (for X-SSH-Port header behind a LB)
	// MCPEnabled controls whether the always-on MCP-over-HTTP endpoint runs.
	// Default true. The endpoint is served on MCPPort.
	MCPEnabled   bool
	MCPPort      int
	TLSCert      string
	TLSKey       string
	CAKeysFile   string
	HostCertFile string
	Files        FilesConfig
	Folders      []FolderConfig
	Passkeys     PasskeysConfig
	Logger       *slog.Logger

	// Readonly is the global write lock. Default true: the substrate is a
	// read-only filesystem and no write verbs are available. Set false to
	// enable the experimental writable substrate (SetWriteable is called at
	// startup). Global readonly is a hard physical lock — a per-docset
	// readonly=false cannot loosen it.
	Readonly bool

	// Track sources for conflict detection.
	configFileLoaded   bool
	embeddedConfigUsed bool
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
	Lore            map[string][]string   `json:"lore"`
	Identities      []AuthIdentity        `json:"identities"`
}

// DocsetSpec defines a named set of path mappings.
type DocsetSpec struct {
	Paths          []PathMapping `json:"paths"`
	PublishDir     string        `json:"publish_dir,omitempty"`
	MaxPublishSize int64         `json:"max_publish_size,omitempty"` // bytes; 0 = use default (2.5MB)

	// Readonly is the per-docset policy check (enforced in the write pipeline,
	// not on the substrate). nil means "inherit" (writable when the global lock
	// is open). A docset can only further restrict: setting it true blocks
	// writes to this docset even when the global lock is open; setting it false
	// is meaningless when the global lock is closed.
	Readonly *bool `json:"readonly,omitempty"`
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

// AuthIdentity defines a user identity with access to a lore spec.
type AuthIdentity struct {
	Name      string   `json:"name"`
	Comment   string   `json:"comment,omitempty"`
	PublicKey string   `json:"public_key"`
	Lore      string   `json:"lore"`
	Publish   []string `json:"publish,omitempty"` // writable docsets; empty = all in lore
}

// Option is a functional option for configuring the server.
type Option func(*Config) error

// fileConfig mirrors Config for YAML deserialization.
type fileConfig struct {
	ConfigVersion   string         `yaml:"version"`
	Port            int            `yaml:"port"`
	MetricsPort     int            `yaml:"metrics_port"`
	HostKeyPath     string         `yaml:"host_key_path"`
	MOTD            string         `yaml:"motd"`
	MOTDFile        string         `yaml:"motd_file"`
	AuthFile        string         `yaml:"auth_file"`
	SkillsDir       string         `yaml:"skills_dir"`
	HTTPPort        int            `yaml:"http_port"`
	ExternalSSHPort int            `yaml:"external_ssh_port"`
	MCP             *mcpYAML       `yaml:"mcp"`
	TLSCert         string         `yaml:"tls_cert"`
	TLSKey          string         `yaml:"tls_key"`
	CAKeysFile      string         `yaml:"ca_keys_file"`
	HostCertFile    string         `yaml:"host_cert_file"`
	DefaultCwd      string         `yaml:"default_cwd"`
	Files           *filesYAML     `yaml:"files"`
	Folders         []FolderConfig `yaml:"folders"`
	Passkeys        *passkeysYAML  `yaml:"passkeys"`
	Readonly        *bool          `yaml:"readonly"`
}

type mcpYAML struct {
	Enabled *bool `yaml:"enabled"`
	Port    int   `yaml:"port"`
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
		Port:            2222,
		HTTPPort:        8080,
		MetricsPort:     3000,
		MCPEnabled:      true,
		MCPPort:         8081,
		HostKeyPath:     ".ssh/openlore_ed25519",
		AllowKeyless:    true,
		UnknownIdentity: "allow",
		DefaultCwd:      "/openlore",
		Readonly:        true, // safe default: read-only substrate
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
		if fc.Readonly != nil {
			cfg.Readonly = *fc.Readonly
		}
		applyPasskeysConfig(cfg, fc.Passkeys)
		applyMCPConfig(cfg, fc.MCP)

		return nil
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
			if fc.Readonly != nil {
				cfg.Readonly = *fc.Readonly
			}
			applyPasskeysConfig(cfg, fc.Passkeys)
			applyMCPConfig(cfg, fc.MCP)
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
	if m.Port != 0 {
		cfg.MCPPort = m.Port
	}
}

// WithMCPPort sets the MCP-over-HTTP endpoint port. 0 disables it.
func WithMCPPort(port int) Option {
	return func(cfg *Config) error {
		cfg.MCPPort = port
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

	for _, ident := range auth.Identities {
		if _, ok := auth.Lore[ident.Lore]; !ok {
			return nil, fmt.Errorf("identity %q references unknown lore %q", ident.Name, ident.Lore)
		}
	}

	for loreName, docsetNames := range auth.Lore {
		for _, ds := range docsetNames {
			if _, ok := auth.Docsets[ds]; !ok {
				return nil, fmt.Errorf("lore %q references unknown docset %q", loreName, ds)
			}
		}
	}

	return &auth, nil
}
