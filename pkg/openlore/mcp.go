package openlore

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aakarim/go-openlore/pkg/bashfs"
	"github.com/aakarim/go-openlore/pkg/bashfs/cmds"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPOption configures the MCP server constructed by NewMCPServer.
type MCPOption func(*mcpConfig)

type mcpConfig struct {
	serverName       string
	instructions     string
	shellDescription string
	envVars          map[string]string
}

// WithMCPServerName overrides the MCP server name reported in the initialize
// response. Clients see this in their connector list.
func WithMCPServerName(name string) MCPOption {
	return func(c *mcpConfig) { c.serverName = name }
}

// WithMCPInstructions sets server instructions that are automatically injected
// into the client's context when it connects.
func WithMCPInstructions(instructions string) MCPOption {
	return func(c *mcpConfig) { c.instructions = instructions }
}

// WithMCPShellDescription overrides the shell tool's description.
func WithMCPShellDescription(desc string) MCPOption {
	return func(c *mcpConfig) { c.shellDescription = desc }
}

// WithMCPEnvVars sets environment variables on the shell for every command
// execution.
func WithMCPEnvVars(vars map[string]string) MCPOption {
	return func(c *mcpConfig) { c.envVars = vars }
}

// NewMCPServer creates an MCP server backed by the given filesystem. The
// returned server exposes two tools — `shell` and `list_commands` — that let
// agents browse and operate on the filesystem via a restricted shell.
func NewMCPServer(fs bashfs.FileSystem, opts ...MCPOption) *mcp.Server {
	var cfg mcpConfig
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
		&mcp.Implementation{Name: serverName, Version: "1.0.0"},
		serverOpts,
	)

	shellDesc := "Execute a bash command against the knowledge base filesystem. Supports ls, cat, grep, find, tree, head, tail, wc, stat, sort, uniq, cut, sed, awk, jq, xargs, pipes, loops, and more."
	if cfg.shellDescription != "" {
		shellDesc = cfg.shellDescription
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "shell",
		Description: shellDesc,
	}, newMCPShellHandler(fs, cfg.envVars))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_commands",
		Description: "List all available shell commands.",
	}, newMCPListCommandsHandler())

	return server
}

type mcpShellInput struct {
	Command string `json:"command" jsonschema:"The bash command to execute (e.g. grep -r auth /docs)"`
}

func newMCPShellHandler(fs bashfs.FileSystem, envVars map[string]string) mcp.ToolHandlerFor[mcpShellInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input mcpShellInput) (*mcp.CallToolResult, any, error) {
		if input.Command == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "command is required"}},
				IsError: true,
			}, nil, nil
		}

		shell := bashfs.NewShell(fs)
		for k, v := range envVars {
			shell.SetEnv(k, v)
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

type mcpListCommandsInput struct{}

func newMCPListCommandsHandler() mcp.ToolHandlerFor[mcpListCommandsInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input mcpListCommandsInput) (*mcp.CallToolResult, any, error) {
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
