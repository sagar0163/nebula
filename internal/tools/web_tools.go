package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	htmlTagRegex = regexp.MustCompile(`<[^>]*>`)
	spaceRegex   = regexp.MustCompile(`\s+`)
)

type WebFetchTool struct{}

func (t *WebFetchTool) Name() string { return "WebFetchTool" }
func (t *WebFetchTool) Description() string { return "Fetches and extracts text content from a URL." }
func (t *WebFetchTool) Parameters() map[string]string {
	return map[string]string{"url": "The URL to fetch (http/https only)"}
}
func (t *WebFetchTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	rawURL := input["url"]
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("invalid scheme: only http/https allowed")
	}

	// 10s timeout as per requirements
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Nebula-Agent/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // read up to 1MB
	if err != nil {
		return "", err
	}

	text := stripHTML(string(body))
	if len(text) > 4000 {
		text = text[:4000] + "\n...[truncated 4KB limit]"
	}

	return text, nil
}

type WebSearchTool struct{}

func (t *WebSearchTool) Name() string { return "WebSearchTool" }
func (t *WebSearchTool) Description() string { return "Searches the web using DuckDuckGo." }
func (t *WebSearchTool) Parameters() map[string]string {
	return map[string]string{"query": "The search query"}
}
func (t *WebSearchTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	query := input["query"]
	if query == "" {
		return "", fmt.Errorf("empty query")
	}

	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, searchURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)") // DDG blocks bots if obvious

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	html := string(body)

	// Extract snippets: extremely naive extraction for demo
	// In production, we'd use golang.org/x/net/html
	snippetRegex := regexp.MustCompile(`(?s)<a class="result__snippet[^>]*>(.*?)</a>`)
	matches := snippetRegex.FindAllStringSubmatch(html, 5)

	if len(matches) == 0 {
		return "No results found or rate limited.", nil
	}

	var results []string
	for i, m := range matches {
		cleanSnippet := stripHTML(m[1])
		results = append(results, fmt.Sprintf("%d. %s", i+1, cleanSnippet))
	}

	return strings.Join(results, "\n"), nil
}

func stripHTML(html string) string {
	// naive HTML strip
	text := htmlTagRegex.ReplaceAllString(html, " ")
	text = spaceRegex.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}
