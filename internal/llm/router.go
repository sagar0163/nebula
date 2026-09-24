package llm

import (
	"context"
	"fmt"
)

// Router selects a provider based on workload and availability,
// with fallback across the registered provider chain.
type Router struct {
	providers map[Workload][]Provider
}

// NewRouter returns a Router. Providers are tried in slice order per workload.
func NewRouter() *Router {
	return &Router{
		providers: make(map[Workload][]Provider),
	}
}

// Register adds a provider for the given workload tier.
// Providers registered first have higher priority.
func (r *Router) Register(w Workload, p Provider) {
	r.providers[w] = append(r.providers[w], p)
}

// Complete routes the request to the best available provider for the workload.
// If an available provider returns an error, the router falls through to the
// next available provider and surfaces the last error only when all fail.
func (r *Router) Complete(ctx context.Context, w Workload, req Request) (<-chan Token, error) {
	chain := r.providers[w]
	if len(chain) == 0 {
		// fall back to any available provider
		for _, providers := range r.providers {
			chain = append(chain, providers...)
		}
	}

	var lastErr error
	for _, p := range chain {
		if !p.Available(ctx) {
			continue
		}
		ch, err := p.Complete(ctx, req)
		if err != nil {
			lastErr = err
			continue
		}
		return ch, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no available LLM provider for workload %d", w)
}

// Embed routes to the first available embed-capable provider, falling through
// on per-provider errors and surfacing the last error when all fail.
func (r *Router) Embed(ctx context.Context, text string) ([]float32, error) {
	var lastErr error
	for _, p := range r.providers[WorkloadEmbed] {
		if !p.Available(ctx) {
			continue
		}
		vec, err := p.Embed(ctx, text)
		if err != nil {
			lastErr = err
			continue
		}
		return vec, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no available embedding provider")
}
