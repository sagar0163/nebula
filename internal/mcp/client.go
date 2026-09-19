package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// ServerConfig describes a single MCP server entry from config.
type ServerConfig struct {
	Name    string            `toml:"name"`
	Type    string            `toml:"type"` // "stdio" | "http" | "sse"
	Command string            `toml:"command"`
	Args    []string          `toml:"args"`
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
	mu      sync.RWMutex
	servers map[string]*serverConn
	tools   map[string]*Tool // "server::tool" → Tool
}

type serverConn struct {
	config ServerConfig
	client *mcpclient.Client
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

// Connect initialises connections to all configured MCP servers concurrently
// and discovers their tools. A single server failing is non-fatal.
func (m *Manager) Connect(ctx context.Context) error {
	var wg sync.WaitGroup
	for name, conn := range m.servers {
		wg.Add(1)
		go func(name string, conn *serverConn) {
			defer wg.Done()
			if err := m.connectServer(ctx, name, conn); err != nil {
				fmt.Printf("mcp: failed to connect to %s: %v\n", name, err)
			}
		}(name, conn)
	}
	wg.Wait()
	return nil
}

func (m *Manager) connectServer(ctx context.Context, name string, conn *serverConn) error {
	cfg := conn.config

	var c *mcpclient.Client
	var err error

	switch cfg.Type {
	case "stdio":
		if cfg.Command == "" {
			return fmt.Errorf("stdio server %q has no command", name)
		}
		env := envSlice(cfg.Env)
		c, err = mcpclient.NewStdioMCPClient(cfg.Command, env, cfg.Args...)
		if err != nil {
			return fmt.Errorf("start stdio client: %w", err)
		}

	case "http":
		if cfg.URL == "" {
			return fmt.Errorf("http server %q has no url", name)
		}
		c, err = mcpclient.NewStreamableHttpClient(cfg.URL)
		if err != nil {
			return fmt.Errorf("create http client: %w", err)
		}

	case "sse":
		if cfg.URL == "" {
			return fmt.Errorf("sse server %q has no url", name)
		}
		c, err = mcpclient.NewSSEMCPClient(cfg.URL)
		if err != nil {
			return fmt.Errorf("create sse client: %w", err)
		}

	default:
		return fmt.Errorf("unknown server type %q for %q (want stdio|http|sse)", cfg.Type, name)
	}

	// Initialize the session.
	_, err = c.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "nebula",
				Version: "0.1.0",
			},
		},
	})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	// Discover tools.
	result, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	m.mu.Lock()
	conn.client = c
	for _, t := range result.Tools {
		key := name + "::" + t.Name
		m.tools[key] = &Tool{
			ServerName:  name,
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	m.mu.Unlock()

	fmt.Printf("mcp: connected to %s (%d tools)\n", name, len(result.Tools))
	return nil
}

// CallTool invokes a tool by its namespaced key ("server::tool").
func (m *Manager) CallTool(ctx context.Context, key string, args map[string]any) (*CallResult, error) {
	m.mu.RLock()
	tool, ok := m.tools[key]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("mcp: tool not found: %s", key)
	}

	m.mu.RLock()
	conn, ok := m.servers[tool.ServerName]
	m.mu.RUnlock()
	if !ok || conn.client == nil {
		return nil, fmt.Errorf("mcp: server %q not connected", tool.ServerName)
	}

	result, err := conn.client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      tool.Name,
			Arguments: args,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("mcp: call %s: %w", key, err)
	}

	return &CallResult{
		Content: extractText(result.Content),
		IsError: result.IsError,
	}, nil
}

// ListTools returns all discovered tools across all connected servers.
func (m *Manager) ListTools() []*Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tools := make([]*Tool, 0, len(m.tools))
	for _, t := range m.tools {
		tools = append(tools, t)
	}
	return tools
}

// ── helpers ───────────────────────────────────────────────────────────────

// envSlice converts a map to the KEY=VALUE slice exec expects.
func envSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// extractText concatenates all TextContent items from a tool result.
func extractText(contents []mcp.Content) string {
	var parts []string
	for _, c := range contents {
		if tc, ok := c.(mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}
