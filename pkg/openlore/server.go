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
	"strings"
	"time"

	"github.com/aakarim/go-openlore/assets"
	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/internal/httpserver"
	"github.com/aakarim/go-openlore/internal/metrics"
	"github.com/aakarim/go-openlore/internal/passkeys"
	"github.com/aakarim/go-openlore/internal/skills"
	"github.com/aakarim/go-openlore/pkg/bashfs/cmds"
	"github.com/aakarim/go-openlore/pkg/bashfs"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/logging"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

// SessionFSFn returns the filesystem to use for a given SSH session identity.
// The default implementation returns the base FS unchanged.
type SessionFSFn func(id Identity, base bashfs.FileSystem) bashfs.FileSystem

// Server is the main OpenLore SSH server.
type Server struct {
	config   config.Config
	auth     *config.AuthConfig
	fs       bashfs.FileSystem
	merge    *MergeFS
	metrics  *metrics.Metrics
	srv      *ssh.Server
	httpSrv  *httpserver.Server
	passkeys *passkeys.Passkeys
	logger   *slog.Logger
	motd     string

	onConnect    OnConnectFunc
	onDisconnect OnDisconnectFunc
	sessionFSFn  SessionFSFn
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
		config:  cfg,
		merge:   NewMergeFS(),
		metrics: &metrics.Metrics{},
		logger:  logger,
		motd:    cfg.MOTD,
	}

	// Load auth config
	if cfg.AuthFile != "" {
		auth, err := config.LoadAuthConfig(cfg.AuthFile)
		if err != nil {
			return nil, fmt.Errorf("loading auth config: %w", err)
		}
		s.auth = auth

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
				cmds.RegisterPublishTarget(name, ds.PublishDir, ds.MaxPublishSize)
			}
		}
	}

	// Set up root directory
	if rootDir != "" {
		rootFS := NewDirFS(rootDir, cfg.Files)
		s.merge.SetRoot(rootFS)
	}

	// Set up additional folders
	for _, folder := range cfg.Folders {
		folderFS := NewDirFS(folder.Path, cfg.Files)
		s.merge.Mount(folder.Name, folderFS)
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

		if s.auth != nil {
			pk.SetAuthConfig(s.auth)
		}
	}

	return s, nil
}

// Config returns the resolved configuration.
func (s *Server) Config() config.Config {
	return s.config
}

// Mount adds a named filesystem mount point using a bashfs.FileSystem.
func (s *Server) Mount(name string, fs bashfs.FileSystem) {
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

// SetRootBashFS sets the root filesystem using a bashfs.FileSystem.
// Paths that don't match any mount fall through to this filesystem.
func (s *Server) SetRootBashFS(fsys bashfs.FileSystem) {
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
func (s *Server) FileSystem() bashfs.FileSystem {
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
		PublicKey:    sess.PublicKey(),
		SessionID:   generateSessionID(),
		ConnectedAt: time.Now(),
	}

	// Resolve path access from auth config
	if s.auth != nil && id.PublicKey != nil {
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
				id.IdentityName = ident.Name
				id.LoreName = ident.Lore
				id.PathAccess = s.resolveLorePathAccess(ident.Lore)
				id.PublishDocsets = ident.Publish
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
	} else if s.auth != nil {
		// Keyless connection with auth config: use "default" lore
		if _, ok := s.auth.Lore["default"]; ok {
			id.LoreName = "default"
			id.PathAccess = s.resolveLorePathAccess("default")
		}
	} else {
		// No auth config at all: full access
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
	if s.auth == nil {
		return nil
	}
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

		// Build per-session filesystem filtered by lore
		sessionFS := bashfs.FileSystem(s.merge)
		if id.LoreName != "" && s.auth != nil {
			if docsetNames, ok := s.auth.Lore[id.LoreName]; ok {
				allowed := make(map[string]bool)
				for _, ds := range docsetNames {
					allowed[ds] = true
				}
				sessionFS = s.merge.FilteredView(allowed)
			}
		}
		if s.sessionFSFn != nil {
			sessionFS = s.sessionFSFn(id, sessionFS)
		}

		shell := bashfs.NewShell(sessionFS)
		if s.config.DefaultCwd != "" {
			shell.SetCwd(s.config.DefaultCwd)
		}

		// Set identity info as environment variables
		if id.IdentityName != "" {
			shell.SetEnv("OPENLORE_IDENTITY", id.IdentityName)
		}
		// Expose the SSH user as the agent ID so commands like `kb` and
		// the writable VFS can scope operations per-agent.
		if id.User != "" {
			shell.SetEnv("OPENLORE_USER", id.User)
			shell.SetEnv("OPENLORE_AGENT_ID", id.User)
		}
		if id.LoreName != "" {
			shell.SetEnv("OPENLORE_LORE", id.LoreName)
			// Set writable docsets for publish scoping
			if len(id.PublishDocsets) > 0 {
				// Identity has explicit publish scope
				shell.SetEnv("OPENLORE_DOCSETS", strings.Join(id.PublishDocsets, ","))
			} else if s.auth != nil {
				// Fall back to all docsets in lore
				if docsetNames, ok := s.auth.Lore[id.LoreName]; ok {
					shell.SetEnv("OPENLORE_DOCSETS", strings.Join(docsetNames, ","))
				}
			}
		}

		cmd := sess.Command()
		if len(cmd) > 0 {
			cmdLine := joinCommand(cmd)
			s.metrics.TotalCommands.Add(1)
			exitCode := shell.ExecPipeline(cmdLine, sess, sess.Stderr(), sess)
			sess.Exit(exitCode)
			return
		}

		shell.RunInteractive(sess, sess, s.motd, "lore")
		sess.Exit(0)
	}
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
		// Truly keyless: we don't set PublicKeyHandler so gliderlabs/ssh
		// flips NoClientAuth=true (see gliderlabs/ssh/server.go:
		// "if PasswordHandler==nil && PublicKeyHandler==nil && ..."). This
		// lets clients without any SSH key (fresh containers, CI runners,
		// people running `ssh` from a brand-new VM) connect anonymously.
		// Clients that *do* have keys still connect fine — they just end up
		// authenticating with the `none` method instead of `publickey`.
	} else {
		opts = append(opts, ssh.PublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			if s.config.UnknownIdentity == "deny" && s.auth != nil {
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
				Port:        s.config.HTTPPort,
				TLSCert:     s.config.TLSCert,
				TLSKey:      s.config.TLSKey,
				HostKeyPath: s.config.HostKeyPath,
				SSHPort:     s.advertisedSSHPort(),
				Logger:      s.logger,
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

				httpCfg.Extenders = append(httpCfg.Extenders, s.passkeys)

				lorePath := s.config.Passkeys.LorePath
				if lorePath == "" {
					lorePath = "/lore"
				}
				lorePath = "/" + strings.Trim(lorePath, "/")
				httpCfg.ExtraHandlers = map[string]http.Handler{
					lorePath + "/": s.passkeys.LoreBrowserHandler(s.fs),
				}

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
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}
