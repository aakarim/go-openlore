package openlore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"github.com/aakarim/go-openlore/assets"
	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/internal/httpserver"
	"github.com/aakarim/go-openlore/internal/metrics"
	"github.com/aakarim/go-openlore/internal/skills"
	"github.com/aakarim/go-openlore/pkg/bashfs/cmds"
	"github.com/aakarim/go-openlore/pkg/bashfs"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/logging"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

// Server is the main OpenLore SSH server.
type Server struct {
	config   config.Config
	auth     *config.AuthConfig
	fs       bashfs.FileSystem
	merge    *MergeFS
	metrics  *metrics.Metrics
	srv      *ssh.Server
	httpSrv  *httpserver.Server
	logger   *slog.Logger
	motd     string

	onConnect    OnConnectFunc
	onDisconnect OnDisconnectFunc
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
	}

	// Set up root directory
	if rootDir != "" {
		rootFS := NewDirFS(rootDir, cfg.Files)
		s.merge.Mount("docs", rootFS)
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
		keyStr := string(gossh.MarshalAuthorizedKey(id.PublicKey))
		matched := false
		for _, ident := range s.auth.Identities {
			if ident.PublicKey == keyStr {
				if spec, ok := s.auth.Lore[ident.Lore]; ok {
					for _, pm := range spec.Paths {
						id.PathAccess = append(id.PathAccess, pm)
					}
				}
				id.LoreName = ident.Lore
				matched = true
				break
			}
		}
		// Unrecognized key: fall back to "default" lore spec
		if !matched {
			if spec, ok := s.auth.Lore["default"]; ok {
				id.LoreName = "default"
				for _, pm := range spec.Paths {
					id.PathAccess = append(id.PathAccess, pm)
				}
			}
		}
	} else if s.auth != nil {
		// Keyless connection with auth config: use "default" lore
		if spec, ok := s.auth.Lore["default"]; ok {
			id.LoreName = "default"
			for _, pm := range spec.Paths {
				id.PathAccess = append(id.PathAccess, pm)
			}
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

		shell := bashfs.NewShell(s.fs)
		if s.config.DefaultCwd != "" {
			shell.SetCwd(s.config.DefaultCwd)
		}

		cmd := sess.Command()
		if len(cmd) > 0 {
			cmdLine := joinCommand(cmd)
			s.metrics.TotalCommands.Add(1)
			exitCode := shell.ExecPipeline(cmdLine, sess, sess.Stderr())
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
	}

	if !s.config.AllowKeyless {
		opts = append(opts, ssh.PublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			if s.config.UnknownIdentity == "deny" && s.auth != nil {
				keyStr := string(gossh.MarshalAuthorizedKey(key))
				for _, ident := range s.auth.Identities {
					if ident.PublicKey == keyStr {
						return true
					}
				}
				return false
			}
			return true
		}))
	}

	srv, err := wish.NewServer(opts...)
	if err != nil {
		return fmt.Errorf("creating SSH server: %w", err)
	}
	s.srv = srv

	if s.config.MetricsPort > 0 {
		metrics.StartServer(s.config.MetricsPort, s.metrics, s.logger)
	}

	if s.config.HTTPPort > 0 {
		webFS := assets.Web()
		if webFS != nil {
			httpSrv := httpserver.New(webFS, httpserver.Config{
				Port:    s.config.HTTPPort,
				TLSCert: s.config.TLSCert,
				TLSKey:  s.config.TLSKey,
				Logger:  s.logger,
			})
			httpSrv.Start()
			s.httpSrv = httpSrv
		}
	}

	s.logger.Info("SSH server starting", "port", s.config.Port)
	return s.srv.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}
