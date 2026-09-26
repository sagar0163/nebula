package tools

import (
	"context"
	"fmt"
)

type WebFetchTool struct{}

func (t *WebFetchTool) Name() string { return "WebFetchTool" }
func (t *WebFetchTool) Description() string { return "Fetches content from a URL." }
func (t *WebFetchTool) Parameters() map[string]string {
	return map[string]string{"url": "The URL to fetch"}
}
func (t *WebFetchTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	return fmt.Sprintf("Fetched content from %s (mocked)", input["url"]), nil
}

type WebSearchTool struct{}

func (t *WebSearchTool) Name() string { return "WebSearchTool" }
func (t *WebSearchTool) Description() string { return "Searches the web for a query." }
func (t *WebSearchTool) Parameters() map[string]string {
	return map[string]string{"query": "The search query"}
}
func (t *WebSearchTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	return fmt.Sprintf("Search results for %s (mocked)", input["query"]), nil
}
