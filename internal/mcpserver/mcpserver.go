package mcpserver

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

// New creates an MCP server backed by the given filesystem.
func New(fs bashfs.FileSystem) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "openlore",
			Version: "1.0.0",
		},
		nil,
	)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "shell",
		Description: "Execute a bash command against the documentation filesystem. Supports: ls, cat, grep, find, tree, head, tail, wc, stat, sort, uniq, cut, sed, awk, jq, xargs, pipes, loops, and more. The filesystem is read-only.",
	}, newShellHandler(fs))

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

func newShellHandler(fs bashfs.FileSystem) mcp.ToolHandlerFor[shellInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input shellInput) (*mcp.CallToolResult, any, error) {
		if input.Command == "" {
			return toolError("command is required")
		}

		shell := bashfs.NewShell(fs)
		var stdout, stderr bytes.Buffer
		exitCode := shell.ExecPipeline(input.Command, &stdout, &stderr)

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
