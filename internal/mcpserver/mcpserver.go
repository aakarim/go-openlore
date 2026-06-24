package mcpserver

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aakarim/go-openlore/pkg/shell"
	"github.com/aakarim/go-openlore/pkg/shell/cmds"
	"github.com/aakarim/go-openlore/pkg/vfs"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AuthIdentityInfo represents a resolved identity from a bearer token.
type AuthIdentityInfo struct {
	Subject string
	Name    string
	Claims  map[string]string
}

// AuthResolver resolves bearer tokens into identities.
type AuthResolver interface {
	Resolve(token string) (*AuthIdentityInfo, error)
}

// Option configures the MCP server.
type Option func(*serverConfig)

type serverConfig struct {
	authResolver     AuthResolver
	instructions     string
	envVars          map[string]string
	serverName       string
	shellDescription string
}

// WithServerName overrides the MCP server name reported in the initialize response.
// This is what clients see in their connector list, so it should clearly describe
// what the server provides (e.g. "Company Knowledge Base").
func WithServerName(name string) Option {
	return func(c *serverConfig) {
		c.serverName = name
	}
}

// WithAuthResolver configures the MCP server with an auth resolver.
func WithAuthResolver(resolver AuthResolver) Option {
	return func(c *serverConfig) {
		c.authResolver = resolver
	}
}

// WithInstructions sets server instructions that are automatically injected
// into the client's context when it connects. This is the MCP-native way to
// teach clients how to use the server — no plugin or skill needed.
func WithInstructions(instructions string) Option {
	return func(c *serverConfig) {
		c.instructions = instructions
	}
}

// WithShellDescription overrides the shell tool's description.
func WithShellDescription(desc string) Option {
	return func(c *serverConfig) {
		c.shellDescription = desc
	}
}

// WithEnvVars sets environment variables on the shell for every command execution.
func WithEnvVars(vars map[string]string) Option {
	return func(c *serverConfig) {
		c.envVars = vars
	}
}

// New creates an MCP server backed by the given filesystem.
func New(fs vfs.FileSystem, opts ...Option) *mcp.Server {
	var cfg serverConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	var serverOpts *mcp.ServerOptions
	if cfg.instructions != "" {
		serverOpts = &mcp.ServerOptions{Instructions: cfg.instructions}
	}

	serverName := "openlore"
	if cfg.serverName != "" {
		serverName = cfg.serverName
	}
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    serverName,
			Version: "1.0.0",
		},
		serverOpts,
	)

	shellDesc := "Execute a bash command against the knowledge base. Use to search, recall, and save company knowledge — including facts, people, projects, invoices, decisions, and any domain data previously stored. Commands: kb search, kb recall, kb save, kb proposals, kb accept, kb reject. Also supports: ls, cat, grep, find, tree, and more for browsing stored knowledge at /kb/, /ontology/, /wiki/, /spaces/."
	if cfg.shellDescription != "" {
		shellDesc = cfg.shellDescription
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "shell",
		Description: shellDesc,
	}, newShellHandler(fs, cfg.authResolver, cfg.envVars))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_commands",
		Description: "List all available shell commands.",
	}, newListCommandsHandler())

	return server
}

func toolError(msg string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}, nil, nil
}

// --- shell tool ---

type shellInput struct {
	Command string `json:"command" jsonschema:"The bash command to execute (e.g. grep -r auth /docs)"`
}

func newShellHandler(fs vfs.FileSystem, auth AuthResolver, envVars map[string]string) mcp.ToolHandlerFor[shellInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input shellInput) (*mcp.CallToolResult, any, error) {
		if input.Command == "" {
			return toolError("command is required")
		}

		shell := shell.NewShell(fs)

		// Apply static env vars (e.g., agent_id from HTTP auth middleware)
		for k, v := range envVars {
			shell.SetEnv(k, v)
		}

		if identity, ok := AuthIdentityFromContext(ctx); ok {
			shell.SetEnv("AUTH_SUBJECT", identity.Subject)
			shell.SetEnv("AUTH_NAME", identity.Name)
			for k, v := range identity.Claims {
				shell.SetEnv("AUTH_CLAIM_"+strings.ToUpper(k), v)
			}
		}
		var stdout, stderr bytes.Buffer
		exitCode := shell.ExecPipeline(input.Command, &stdout, &stderr, nil)

		result := stdout.String()
		if stderr.Len() > 0 {
			result += "\n" + stderr.String()
		}
		if exitCode != 0 {
			result += fmt.Sprintf("\nexit code: %d", exitCode)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: result}},
		}, nil, nil
	}
}

// --- list_commands tool ---

type listCommandsInput struct{}

func newListCommandsHandler() mcp.ToolHandlerFor[listCommandsInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input listCommandsInput) (*mcp.CallToolResult, any, error) {
		names := make([]string, 0, len(cmds.Registry))
		for name := range cmds.Registry {
			names = append(names, name)
		}
		sort.Strings(names)

		var sb strings.Builder
		sb.WriteString("Available commands:\n")
		for _, name := range names {
			sb.WriteString("  ")
			sb.WriteString(name)
			sb.WriteString("\n")
		}
		sb.WriteString("\nShell syntax: pipes (|), && / ||, for/while/if, variables, subshells")
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
		}, nil, nil
	}
}
