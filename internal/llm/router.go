package llm

import (
	"context"
	"fmt"
	"os"
	"strings"
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

	timeoutSec := viper.GetInt("llm.timeout_seconds")
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	timeout := time.Duration(timeoutSec) * time.Second

	var lastErr error
	for _, p := range chain {
		if !p.Available(ctx) {
			continue
		}

		req.Workload = w

		retries := 0
		backoff := 2 * time.Second

		for {
			pCtx, cancel := context.WithTimeout(ctx, timeout)
			ch, err := p.Complete(pCtx, req)

			var streamErr error
			var firstToken Token
			var ok bool

			if err != nil {
				streamErr = err
				lastErr = streamErr
			} else {
				firstToken, ok = <-ch
				if !ok {
					streamErr = nil // stream closed ok immediately
				} else if firstToken.Err != nil {
					streamErr = firstToken.Err
				}
			}

			if streamErr != nil {
				cancel()

				msg := streamErr.Error()
				if isRL(msg) && retries < 3 {
					fmt.Fprintf(os.Stderr, "llm: provider %s rate limited, retrying in %v...\n", p.Name(), backoff)
					select {
					case <-time.After(backoff):
					case <-ctx.Done():
						return nil, ctx.Err()
					}
					retries++
					backoff *= 2
					continue
				}

				fmt.Fprintf(os.Stderr, "llm: provider %s failed: %v, falling back\n", p.Name(), streamErr)
				break // fallback to next provider
			}

			if !ok {
				cancel()
				empty := make(chan Token)
				close(empty)
				return empty, nil
			}

			out := make(chan Token, 32)
			go func() {
				defer close(out)
				defer cancel()
				out <- firstToken
				for x := range ch {
					out <- x
				}
			}()

			return out, nil
		}
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

func isRL(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "429") || strings.Contains(msg, "rate limit")
}
