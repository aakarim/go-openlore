package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/aakarim/go-openlore/assets"
	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/internal/mcpserver"
	"github.com/aakarim/go-openlore/pkg/bashfs/cmds"
	openlore "github.com/aakarim/go-openlore/pkg/openlore"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	gossh "golang.org/x/crypto/ssh"
)

func main() {
	// Handle subcommands before flag parsing
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Printf("openlore %s\n", assets.Version())
			os.Exit(0)
		case "export":
			exportCmd := flag.NewFlagSet("export", flag.ExitOnError)
			outputDir := exportCmd.String("o", "", "output directory (required)")
			exportCmd.StringVar(outputDir, "output", "", "output directory (required)")
			exportCmd.Usage = func() {
				fmt.Fprintf(os.Stderr, "Usage: openlore export -o <directory>\n\n")
				fmt.Fprintf(os.Stderr, "Export embedded documentation to a local directory.\n\n")
				exportCmd.PrintDefaults()
			}
			exportCmd.Parse(os.Args[2:])

			if *outputDir == "" {
				exportCmd.Usage()
				os.Exit(1)
			}

			loreFS := assets.Lore()
			if loreFS == nil {
				fmt.Fprintln(os.Stderr, "error: no embedded docs found")
				os.Exit(1)
			}

			count := 0
			err := fs.WalkDir(loreFS, ".", func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}

				outPath := filepath.Join(*outputDir, p)

				if d.IsDir() {
					return os.MkdirAll(outPath, 0755)
				}

				data, err := fs.ReadFile(loreFS, p)
				if err != nil {
					return fmt.Errorf("reading %s: %w", p, err)
				}

				if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
					return err
				}

				if err := os.WriteFile(outPath, data, 0644); err != nil {
					return fmt.Errorf("writing %s: %w", outPath, err)
				}

				fmt.Printf("  %s\n", p)
				count++
				return nil
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("\nExported %d files to %s\n", count, *outputDir)
			os.Exit(0)
		case "mcp":
			mcpCmd := flag.NewFlagSet("mcp", flag.ExitOnError)
			mcpAllowed := mcpCmd.String("allowed", "", "comma-separated file patterns (e.g. '*.md,*.txt')")
			mcpIgnore := mcpCmd.String("ignore", "", "comma-separated ignore patterns (e.g. '.git,node_modules')")
			mcpConfig := mcpCmd.String("config", "./openlore.yml", "path to config file")
			mcpCmd.StringVar(mcpConfig, "c", "./openlore.yml", "path to config file (shorthand)")
			mcpCmd.Usage = func() {
				fmt.Fprintf(os.Stderr, "Usage: openlore mcp [flags] [directory]\n\n")
				fmt.Fprintf(os.Stderr, "Run as an MCP server over stdio. Exposes documentation\n")
				fmt.Fprintf(os.Stderr, "via the Model Context Protocol for Claude Desktop, Cowork, etc.\n\n")
				fmt.Fprintf(os.Stderr, "Arguments:\n")
				fmt.Fprintf(os.Stderr, "  directory    Directory to serve (default: embedded docs)\n\n")
				fmt.Fprintf(os.Stderr, "Flags:\n")
				mcpCmd.PrintDefaults()
			}
			mcpCmd.Parse(os.Args[2:])

			// Build filesystem
			var files config.FilesConfig
			if *mcpAllowed != "" {
				files.Allowed = splitAndTrim(*mcpAllowed)
			}
			if *mcpIgnore != "" {
				files.Ignore = splitAndTrim(*mcpIgnore)
			}

			// Try loading config file for file filters
			embeddedCfg, _ := assets.EmbeddedConfig()
			cfgOpts := []config.Option{
				config.WithConfigFile(*mcpConfig),
				config.WithEmbeddedConfig(embeddedCfg, ""),
			}
			if cfg, err := config.New(cfgOpts...); err == nil {
				if len(files.Allowed) == 0 {
					files.Allowed = cfg.Files.Allowed
				}
				if len(files.Ignore) == 0 {
					files.Ignore = cfg.Files.Ignore
				}
			}

			var vfs openlore.FileSystem
			if mcpCmd.NArg() > 0 {
				dir := mcpCmd.Arg(0)
				absDir, err := filepath.Abs(dir)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error: invalid directory %q: %v\n", dir, err)
					os.Exit(1)
				}
				vfs = openlore.NewDirFS(absDir, files)
			} else if loreFS := assets.Lore(); loreFS != nil {
				vfs = openlore.NewFSAdapter(loreFS)
			} else {
				fmt.Fprintln(os.Stderr, "error: no directory specified and no embedded docs found")
				mcpCmd.Usage()
				os.Exit(1)
			}

			server := mcpserver.New(vfs)
			if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
				fmt.Fprintf(os.Stderr, "mcp server error: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0)

		case "mcpb":
			mcpbCmd := flag.NewFlagSet("mcpb", flag.ExitOnError)
			output := mcpbCmd.String("o", "", "output .mcpb file path (default: openlore.mcpb)")
			mcpbCmd.StringVar(output, "output", "", "output .mcpb file path")
			name := mcpbCmd.String("name", "openlore", "extension name")
			description := mcpbCmd.String("description", "Access documentation via OpenLore", "extension description")
			docsDir := mcpbCmd.String("docs-dir", "", "directory to embed as docs (optional)")
			mcpbCmd.Usage = func() {
				fmt.Fprintf(os.Stderr, "Usage: openlore mcpb [flags]\n\n")
				fmt.Fprintf(os.Stderr, "Package the current openlore binary as an MCPB desktop extension\n")
				fmt.Fprintf(os.Stderr, "for one-click installation in Claude Desktop.\n\n")
				fmt.Fprintf(os.Stderr, "Flags:\n")
				mcpbCmd.PrintDefaults()
			}
			mcpbCmd.Parse(os.Args[2:])

			if *output == "" {
				*output = "openlore.mcpb"
			}

			buildMCPB(*output, *name, *description, *docsDir)
			os.Exit(0)

		case "identity":
			if len(os.Args) < 3 {
				fmt.Fprintf(os.Stderr, "Usage: openlore identity <command>\n\n")
				fmt.Fprintf(os.Stderr, "Commands:\n")
				fmt.Fprintf(os.Stderr, "  add    Add a public key identity to lore.json\n")
				os.Exit(1)
			}

			switch os.Args[2] {
			case "add":
				idCmd := flag.NewFlagSet("identity add", flag.ExitOnError)
				name := idCmd.String("name", "", "identity name (required)")
				key := idCmd.String("key", "", "SSH public key (required)")
				loreName := idCmd.String("lore", "", "lore spec name (required)")
				authPath := idCmd.String("auth", "./lore.json", "path to lore.json")
				comment := idCmd.String("comment", "", "optional comment")
				idCmd.Usage = func() {
					fmt.Fprintf(os.Stderr, "Usage: openlore identity add [flags]\n\n")
					fmt.Fprintf(os.Stderr, "Add a public key identity to lore.json.\n\n")
					idCmd.PrintDefaults()
				}
				idCmd.Parse(os.Args[3:])

				if *name == "" || *key == "" || *loreName == "" {
					idCmd.Usage()
					os.Exit(1)
				}

				// Validate the public key
				_, _, _, _, err := gossh.ParseAuthorizedKey([]byte(*key))
				if err != nil {
					fmt.Fprintf(os.Stderr, "error: invalid SSH public key: %v\n", err)
					os.Exit(1)
				}

				// Load or create auth config
				var auth openlore.AuthConfig
				data, readErr := os.ReadFile(*authPath)
				if readErr == nil {
					if err := json.Unmarshal(data, &auth); err != nil {
						fmt.Fprintf(os.Stderr, "error: parsing %s: %v\n", *authPath, err)
						os.Exit(1)
					}
				} else if os.IsNotExist(readErr) {
					auth.Lore = make(map[string]openlore.LoreSpec)
					auth.Lore["default"] = openlore.LoreSpec{Paths: []openlore.PathMapping{{Source: "/", Display: "/"}}}
				} else {
					fmt.Fprintf(os.Stderr, "error: reading %s: %v\n", *authPath, readErr)
					os.Exit(1)
				}

				// Check lore spec exists
				if _, ok := auth.Lore[*loreName]; !ok {
					fmt.Fprintf(os.Stderr, "error: lore spec %q not found in %s\n", *loreName, *authPath)
					fmt.Fprintf(os.Stderr, "Available specs: ")
					for k := range auth.Lore {
						fmt.Fprintf(os.Stderr, "%s ", k)
					}
					fmt.Fprintln(os.Stderr)
					os.Exit(1)
				}

				// Check for duplicate keys
				keyStr := strings.TrimSpace(*key) + "\n"
				for _, ident := range auth.Identities {
					if ident.PublicKey == keyStr || strings.TrimSpace(ident.PublicKey) == strings.TrimSpace(*key) {
						fmt.Fprintf(os.Stderr, "error: public key already exists for identity %q\n", ident.Name)
						os.Exit(1)
					}
				}

				// Add identity
				newIdent := openlore.AuthIdentity{
					Name:      *name,
					PublicKey: strings.TrimSpace(*key),
					Lore:      *loreName,
				}
				if *comment != "" {
					newIdent.Comment = *comment
				}
				auth.Identities = append(auth.Identities, newIdent)

				// Write back
				out, err := json.MarshalIndent(auth, "", "  ")
				if err != nil {
					fmt.Fprintf(os.Stderr, "error: marshaling JSON: %v\n", err)
					os.Exit(1)
				}
				if err := os.WriteFile(*authPath, append(out, '\n'), 0644); err != nil {
					fmt.Fprintf(os.Stderr, "error: writing %s: %v\n", *authPath, err)
					os.Exit(1)
				}

				fmt.Printf("Added identity %q with lore spec %q to %s\n", *name, *loreName, *authPath)
				os.Exit(0)

			default:
				fmt.Fprintf(os.Stderr, "Unknown identity command: %s\n", os.Args[2])
				os.Exit(1)
			}
		}
	}

	port := flag.Int("port", 0, "SSH server port (default 2222)")
	flag.IntVar(port, "p", 0, "SSH server port (shorthand)")
	metricsPort := flag.Int("metrics-port", 0, "Prometheus metrics port (0 to disable, default 3000)")
	hostKey := flag.String("host-key", "", "path to host key file (default .ssh/openlore_ed25519)")
	motd := flag.String("motd", "", "inline MOTD string shown on connect")
	motdFile := flag.String("motd-file", "", "path to MOTD file shown on connect")
	authFile := flag.String("auth", "", "path to auth.json for identity-based access control")
	configFile := flag.String("config", "./openlore.yml", "path to config file")
	flag.StringVar(configFile, "c", "./openlore.yml", "path to config file (shorthand)")
	httpPort := flag.Int("http-port", 0, "HTTP front page port (default 8080, 0 to disable)")
	tlsCert := flag.String("tls-cert", "", "TLS certificate file for HTTP server")
	tlsKey := flag.String("tls-key", "", "TLS key file for HTTP server")
	caKeysFile := flag.String("ca-keys", "", "path to trusted CA public keys file for SSH certificate auth")
	hostCertFile := flag.String("host-cert", "", "path to SSH host certificate (signed by CA)")
	skillsDir := flag.String("skills-dir", "", "directory containing runtime skills")
	allowed := flag.String("allowed", "", "comma-separated file patterns to serve (e.g. '*.md,*.txt')")
	ignore := flag.String("ignore", "", "comma-separated patterns to ignore (e.g. '.git,node_modules')")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: openlore [flags] [directory]\n\n")
		fmt.Fprintf(os.Stderr, "Serve your docs to AI agents over SSH.\n\n")
		fmt.Fprintf(os.Stderr, "Arguments:\n")
		fmt.Fprintf(os.Stderr, "  directory    Directory to serve (default: current directory)\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	// Set up structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Determine directory to serve (if provided)
	var rootDir string
	if flag.NArg() > 0 {
		dir := flag.Arg(0)
		absDir, err := filepath.Abs(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid directory %q: %v\n", dir, err)
			os.Exit(1)
		}
		info, err := os.Stat(absDir)
		if err != nil || !info.IsDir() {
			fmt.Fprintf(os.Stderr, "error: %q is not a directory\n", absDir)
			os.Exit(1)
		}
		rootDir = absDir
	}

	// Build config options.
	// 1. Config file (from disk)
	// 2. Embedded config (from assets/config/openlore.yml, if present)
	// 3. CLI flag overrides (always win)
	// Using both a config file and embedded config is an error.
	embeddedCfg, _ := assets.EmbeddedConfig()
	opts := []openlore.Option{
		openlore.WithConfigFile(*configFile),
		openlore.WithEmbeddedConfig(embeddedCfg, assets.DefaultMOTD()),
		openlore.WithLogger(logger),
	}

	if *port != 0 {
		opts = append(opts, openlore.WithPort(*port))
	}
	if isFlagSet("metrics-port") {
		opts = append(opts, openlore.WithMetricsPort(*metricsPort))
	}
	if *hostKey != "" {
		opts = append(opts, openlore.WithHostKeyPath(*hostKey))
	}
	if *motd != "" {
		opts = append(opts, openlore.WithMOTD(*motd))
	}
	if *motdFile != "" {
		opts = append(opts, openlore.WithMOTDFile(*motdFile))
	}
	if *authFile != "" {
		opts = append(opts, openlore.WithAuthFile(*authFile))
	}
	if *allowed != "" {
		opts = append(opts, openlore.WithAllowedPatterns(splitAndTrim(*allowed)))
	}
	if *ignore != "" {
		opts = append(opts, openlore.WithIgnorePatterns(splitAndTrim(*ignore)))
	}
	if isFlagSet("http-port") {
		opts = append(opts, openlore.WithHTTPPort(*httpPort))
	}
	if *tlsCert != "" && *tlsKey != "" {
		opts = append(opts, openlore.WithTLS(*tlsCert, *tlsKey))
	}
	if *caKeysFile != "" {
		opts = append(opts, openlore.WithCAKeysFile(*caKeysFile))
	}
	if *hostCertFile != "" {
		opts = append(opts, openlore.WithHostCertFile(*hostCertFile))
	}
	if *skillsDir != "" {
		opts = append(opts, openlore.WithSkillsDir(*skillsDir))
	}

	// Create server
	srv, err := openlore.NewServer(rootDir, opts...)
	if err != nil {
		slog.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	cmds.VersionString = assets.Version()

	// Mount embedded docs if present and no directory was given on the CLI
	if rootDir == "" {
		if loreFS := assets.Lore(); loreFS != nil {
			srv.SetRootFS(loreFS)
		}
	}

	cfg := srv.Config()

	// Print startup banner
	fmt.Println()
	fmt.Println("  ┌─────────────────────────────────────┐")
	fmt.Println("  │        📜  OpenLore  📜              │")
	fmt.Println("  │    Serve your docs to AI agents      │")
	fmt.Println("  └─────────────────────────────────────┘")
	fmt.Println()
	if rootDir != "" {
		fmt.Printf("  Directory:  %s\n", rootDir)
	} else if assets.Lore() != nil {
		fmt.Printf("  Directory:  (embedded docs)\n")
	}
	fmt.Printf("  SSH:        ssh -p %d localhost\n", cfg.Port)
	if cfg.MetricsPort > 0 {
		fmt.Printf("  Metrics:    http://localhost:%d/metrics\n", cfg.MetricsPort)
	}
	if cfg.HTTPPort > 0 {
		fmt.Printf("  HTTP:       http://localhost:%d\n", cfg.HTTPPort)
	}
	fmt.Println()

	slog.Info("starting openlore",
		"port", cfg.Port,
		"metrics_port", cfg.MetricsPort,
		"allow_keyless", cfg.AllowKeyless,
	)

	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

func isFlagSet(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func buildMCPB(output, name, description, docsDir string) {
	// Find the current binary
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot find current executable: %v\n", err)
		os.Exit(1)
	}
	exe, _ = filepath.EvalSymlinks(exe)

	// Determine platform suffix for the binary name
	binaryName := "openlore"
	if runtime.GOOS == "windows" {
		binaryName = "openlore.exe"
	}

	// Build manifest
	mcpConfig := map[string]any{
		"command": "${__dirname}/server/" + binaryName,
		"args":    []string{"mcp"},
	}

	// If no embedded docs, add user_config for docs directory
	hasEmbeddedDocs := assets.Lore() != nil
	userConfig := map[string]any{}
	if !hasEmbeddedDocs && docsDir == "" {
		userConfig["docs_directory"] = map[string]any{
			"type":        "directory",
			"title":       "Documentation Directory",
			"description": "Select the directory containing your documentation files",
			"required":    true,
		}
		mcpConfig["args"] = []string{"mcp", "${user_config.docs_directory}"}
	}

	// Platform mapping
	platform := "darwin"
	switch runtime.GOOS {
	case "windows":
		platform = "win32"
	case "linux":
		platform = "linux"
	}

	manifest := map[string]any{
		"manifest_version": "0.3",
		"name":             name,
		"version":          assets.Version(),
		"description":      description,
		"author": map[string]string{
			"name": "OpenLore",
			"url":  "https://github.com/aakarim/go-openlore",
		},
		"server": map[string]any{
			"type":        "binary",
			"entry_point": "server/" + binaryName,
			"mcp_config":  mcpConfig,
		},
		"tools": []map[string]string{
			{"name": "shell", "description": "Execute bash commands against the documentation filesystem"},
			{"name": "list_commands", "description": "List all available shell commands"},
		},
		"compatibility": map[string]any{
			"platforms": []string{platform},
		},
	}
	if len(userConfig) > 0 {
		manifest["user_config"] = userConfig
	}

	// Create the .mcpb zip archive
	f, err := os.Create(output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: creating %s: %v\n", output, err)
		os.Exit(1)
	}
	defer f.Close()

	w := zip.NewWriter(f)

	// Write manifest.json
	manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")
	mw, err := w.Create("manifest.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: writing manifest: %v\n", err)
		os.Exit(1)
	}
	mw.Write(manifestJSON)

	// Copy the binary into server/
	binData, err := os.ReadFile(exe)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: reading binary %s: %v\n", exe, err)
		os.Exit(1)
	}
	bh := &zip.FileHeader{
		Name:   "server/" + binaryName,
		Method: zip.Deflate,
	}
	bh.SetMode(0755)
	bw, err := w.CreateHeader(bh)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: adding binary to archive: %v\n", err)
		os.Exit(1)
	}
	bw.Write(binData)

	// If docs-dir is specified, copy docs into server/assets/lore/ so they
	// get picked up if someone rebuilds. But since this is a binary bundle,
	// the binary already has its own embedded docs (or not).
	if docsDir != "" {
		absDocsDir, _ := filepath.Abs(docsDir)
		filepath.WalkDir(absDocsDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(absDocsDir, p)
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			dw, err := w.Create("docs/" + filepath.ToSlash(rel))
			if err != nil {
				return err
			}
			dw.Write(data)
			return nil
		})
	}

	if err := w.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "error: finalizing archive: %v\n", err)
		os.Exit(1)
	}

	info, _ := os.Stat(output)
	fmt.Printf("Created %s (%.1f MB)\n", output, float64(info.Size())/(1024*1024))
	fmt.Println()
	fmt.Println("Install in Claude Desktop:")
	fmt.Println("  Double-click the .mcpb file, or drag it into Claude Desktop")
	fmt.Println()
	fmt.Printf("  Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	if hasEmbeddedDocs {
		fmt.Println("  Docs: embedded in binary")
	} else if docsDir != "" {
		fmt.Printf("  Docs: bundled from %s\n", docsDir)
	} else {
		fmt.Println("  Docs: user will configure directory on install")
	}
}
