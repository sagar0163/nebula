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
func (r *Router) Complete(ctx context.Context, w Workload, req Request) (<-chan Token, error) {
	chain := r.providers[w]
	if len(chain) == 0 {
		// fall back to any available provider
		for _, providers := range r.providers {
			chain = append(chain, providers...)
		}
	}

	for _, p := range chain {
		if p.Available(ctx) {
			return p.Complete(ctx, req)
		}
	}

	return nil, fmt.Errorf("no available LLM provider for workload %d", w)
}

// Embed routes to the first available embed-capable provider.
func (r *Router) Embed(ctx context.Context, text string) ([]float32, error) {
	for _, p := range r.providers[WorkloadEmbed] {
		if p.Available(ctx) {
			return p.Embed(ctx, text)
		}
	}
	return nil, fmt.Errorf("no available embedding provider")
}
