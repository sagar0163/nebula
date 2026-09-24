package llm

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
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

	timeoutSec := viper.GetInt("llm.timeout_seconds")
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	timeout := time.Duration(timeoutSec) * time.Second

	for _, p := range chain {
		if !p.Available(ctx) {
			continue
		}

		req.Workload = w
		pCtx, cancel := context.WithTimeout(ctx, timeout)
		ch, err := p.Complete(pCtx, req)
		if err != nil {
			cancel()
			fmt.Fprintf(os.Stderr, "llm: provider %s failed: %v, falling back\n", p.Name(), err)
			continue
		}

		// Wait for the first token to verify it doesn't immediately error/timeout.
		t, ok := <-ch
		if !ok {
			cancel()
			// Stream closed immediately without error, return empty channel.
			empty := make(chan Token)
			close(empty)
			return empty, nil
		}

		if t.Err != nil {
			cancel()
			fmt.Fprintf(os.Stderr, "llm: provider %s error: %v, falling back\n", p.Name(), t.Err)
			continue
		}

		// First token was successful. Spin up a goroutine to forward it and the rest.
		out := make(chan Token, 32)
		go func() {
			defer close(out)
			defer cancel()
			out <- t
			for x := range ch {
				out <- x
			}
		}()

		return out, nil
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
