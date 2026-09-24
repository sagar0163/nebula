package llm

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubProvider is a configurable mock LLM provider for router stress tests.
type stubProvider struct {
	name      string
	available bool
	err       error
	delay     time.Duration
	response  string

	calls atomic.Int32
}

func (p *stubProvider) Name() string { return p.name }
func (p *stubProvider) Available(context.Context) bool {
	return p.available
}
func (p *stubProvider) Complete(ctx context.Context, _ Request) (<-chan Token, error) {
	p.calls.Add(1)
	if p.err != nil {
		return nil, p.err
	}
	if p.delay > 0 {
		select {
		case <-time.After(p.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ch := make(chan Token, 1)
	ch <- Token{Text: p.response, IsLast: true}
	close(ch)
	return ch, nil
}
func (p *stubProvider) Embed(ctx context.Context, _ string) ([]float32, error) {
	p.calls.Add(1)
	if p.err != nil {
		return nil, p.err
	}
	if p.delay > 0 {
		select {
		case <-time.After(p.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return []float32{0.5, 0.5}, nil
}

func TestRouterNoProviders(t *testing.T) {
	r := NewRouter()

	if _, err := r.Complete(context.Background(), WorkloadHeal, Request{}); err == nil {
		t.Fatal("Complete on empty router returned nil error")
	}
	if _, err := r.Embed(context.Background(), "x"); err == nil {
		t.Fatal("Embed on empty router returned nil error")
	}
}

func TestRouterAllUnavailable(t *testing.T) {
	r := NewRouter()
	r.Register(WorkloadHeal, &stubProvider{name: "a", available: false})
	r.Register(WorkloadHeal, &stubProvider{name: "b", available: false})

	if _, err := r.Complete(context.Background(), WorkloadHeal, Request{}); err == nil {
		t.Fatal("Complete with all-unavailable providers returned nil error")
	}
	if _, err := r.Embed(context.Background(), "x"); err == nil {
		t.Fatal("Embed with no embed providers returned nil error")
	}
}

func TestRouterAllProvidersErrorSurfacesLast(t *testing.T) {
	r := NewRouter()
	a := &stubProvider{name: "a", available: true, err: errors.New("first error")}
	b := &stubProvider{name: "b", available: true, err: errors.New("last error")}
	r.Register(WorkloadHeal, a)
	r.Register(WorkloadHeal, b)

	_, err := r.Complete(context.Background(), WorkloadHeal, Request{})
	if err == nil {
		t.Fatal("Complete returned nil error when all providers failed")
	}
	if err.Error() != "last error" {
		t.Fatalf("Complete err = %q, want the last provider error", err)
	}
	if a.calls.Load() != 1 || b.calls.Load() != 1 {
		t.Fatalf("provider calls a=%d b=%d, want both tried once", a.calls.Load(), b.calls.Load())
	}
}

func TestRouterAllEmbedProvidersErrorSurfacesLast(t *testing.T) {
	r := NewRouter()
	a := &stubProvider{name: "ea", available: true, err: errors.New("embed first")}
	b := &stubProvider{name: "eb", available: true, err: errors.New("embed last")}
	r.Register(WorkloadEmbed, a)
	r.Register(WorkloadEmbed, b)

	_, err := r.Embed(context.Background(), "text")
	if err == nil {
		t.Fatal("Embed returned nil error when all providers failed")
	}
	if err.Error() != "embed last" {
		t.Fatalf("Embed err = %q, want the last provider error", err)
	}
}

func TestRouterFirstProviderTimesOutFallsThrough(t *testing.T) {
	r := NewRouter()
	slow := &stubProvider{name: "slow", available: true, delay: 5 * time.Second}
	fast := &stubProvider{name: "fast", available: true, response: "fallback-ok"}
	r.Register(WorkloadHeal, slow)
	r.Register(WorkloadHeal, fast)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	tokens, err := r.Complete(ctx, WorkloadHeal, Request{})
	if err != nil {
		t.Fatalf("Complete with timed-out first provider: %v", err)
	}
	got := drainTokens(tokens)
	if got != "fallback-ok" {
		t.Fatalf("response = %q, want fallback from fast provider", got)
	}
}

func TestRouterOnlySlowProviderSurfacesTimeout(t *testing.T) {
	r := NewRouter()
	slow := &stubProvider{name: "slow", available: true, delay: 5 * time.Second}
	r.Register(WorkloadHeal, slow)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := r.Complete(ctx, WorkloadHeal, Request{})
	if err == nil {
		t.Fatal("Complete returned nil error for a timing-out provider")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context deadline exceeded", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("timeout path took %v, hung", time.Since(start))
	}
}

func TestRouterConcurrentComplete(t *testing.T) {
	r := NewRouter()
	p := &stubProvider{name: "conc", available: true, response: "ok"}
	r.Register(WorkloadLearn, p)

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tokens, err := r.Complete(context.Background(), WorkloadLearn, Request{})
			if err != nil {
				errs <- err
				return
			}
			if got := drainTokens(tokens); got != "ok" {
				errs <- errors.New("unexpected response: " + got)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Complete error: %v", err)
	}
	if p.calls.Load() != 10 {
		t.Fatalf("provider called %d times, want 10", p.calls.Load())
	}
}

func TestRouterConcurrentEmbed(t *testing.T) {
	r := NewRouter()
	p := &stubProvider{name: "cemb", available: true}
	r.Register(WorkloadEmbed, p)

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vec, err := r.Embed(context.Background(), "some text")
			if err != nil {
				errs <- err
				return
			}
			if len(vec) != 2 || vec[0] != 0.5 {
				errs <- errors.New("unexpected embedding vector")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Embed error: %v", err)
	}
	if p.calls.Load() != 10 {
		t.Fatalf("embed provider called %d times, want 10", p.calls.Load())
	}
}

// drainTokens collects the Text of every token in a stream.
func drainTokens(tokens <-chan Token) string {
	var out string
	for t := range tokens {
		if t.Err != nil {
			return out + t.Err.Error()
		}
		out += t.Text
	}
	return out
}