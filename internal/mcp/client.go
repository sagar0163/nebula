package mcp

import (
	"context"
	"fmt"
)

// ServerConfig describes a single MCP server entry from config.
type ServerConfig struct {
	Name    string            `toml:"name"`
	Type    string            `toml:"type"` // "stdio" | "http"
	Command string            `toml:"command"`
	URL     string            `toml:"url"`
	Env     map[string]string `toml:"env"`
}

// Tool is a discovered MCP tool with its server origin.
type Tool struct {
	ServerName  string
	Name        string
	Description string
	InputSchema any
}

// CallResult is the response from a tool invocation.
type CallResult struct {
	Content string
	IsError bool
}

// Manager manages connections to multiple MCP servers and exposes
// a unified tool registry to the agent.
type Manager struct {
	servers map[string]*serverConn
	tools   map[string]*Tool // "server::tool" → Tool
}

type serverConn struct {
	config ServerConfig
	// TODO: hold the actual MCP client session from mark3labs/mcp-go
	// or modelcontextprotocol/go-sdk once wired up.
}

// NewManager creates a Manager from the given server configs.
func NewManager(configs []ServerConfig) *Manager {
	m := &Manager{
		servers: make(map[string]*serverConn),
		tools:   make(map[string]*Tool),
	}
	for _, cfg := range configs {
		m.servers[cfg.Name] = &serverConn{config: cfg}
	}
	return m
}

// Connect initializes connections to all configured MCP servers
// and discovers their tools.
func (m *Manager) Connect(ctx context.Context) error {
	for name, conn := range m.servers {
		if err := m.connectServer(ctx, name, conn); err != nil {
			// Non-fatal: log and continue. A dead MCP server shouldn't
			// block the whole agent from starting.
			fmt.Printf("mcp: failed to connect to %s: %v\n", name, err)
		}
	}
	return nil
}

func (m *Manager) connectServer(ctx context.Context, name string, conn *serverConn) error {
	// TODO: implement stdio/http transport and tool discovery using
	// github.com/modelcontextprotocol/go-sdk (official SDK, spec 2026-07-28)
	// or github.com/mark3labs/mcp-go as fallback.
	_ = ctx
	_ = name
	_ = conn
	return nil
}

// CallTool invokes a tool by its namespaced key ("server::tool").
func (m *Manager) CallTool(ctx context.Context, key string, args map[string]any) (*CallResult, error) {
	tool, ok := m.tools[key]
	if !ok {
		return nil, fmt.Errorf("mcp: tool not found: %s", key)
	}

	_ = tool
	// TODO: route to the correct server connection and invoke.
	return nil, fmt.Errorf("mcp: tool invocation not yet implemented")
}

// ListTools returns all discovered tools across all connected servers.
// Lazy discovery: call on-demand, not at startup.
func (m *Manager) ListTools() []*Tool {
	tools := make([]*Tool, 0, len(m.tools))
	for _, t := range m.tools {
		tools = append(tools, t)
	}
	return tools
}
