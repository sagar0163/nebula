package llm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/viper"
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

type slowMockProvider struct {
	name string
}

func (p *slowMockProvider) Name() string {
	return p.name
}

func (p *slowMockProvider) Available(ctx context.Context) bool {
	return true
}

func (p *slowMockProvider) Complete(ctx context.Context, req Request) (<-chan Token, error) {
	ch := make(chan Token)
	go func() {
		defer close(ch)
		select {
		case <-ctx.Done():
			ch <- Token{Err: ctx.Err()}
		case <-time.After(5 * time.Second):
			// This shouldn't be reached if timeout is shorter.
			ch <- Token{Text: "too slow"}
		}
	}()
	return ch, nil
}

func (p *slowMockProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, errors.New("unimplemented")
}

func TestRouter_TimeoutFallback(t *testing.T) {
	// Set the timeout very low for testing
	viper.Set("llm.timeout_seconds", 1)
	defer viper.Reset()

	router := NewRouter()
	router.Register(WorkloadHeal, &slowMockProvider{name: "slow1"})
	router.Register(WorkloadHeal, &slowMockProvider{name: "slow2"})

	ctx := context.Background()
	_, err := router.Complete(ctx, WorkloadHeal, Request{})
	if err == nil {
		t.Fatal("expected error from router because all providers timed out")
	}

	if !strings.Contains(err.Error(), "no available LLM provider") {
		t.Fatalf("expected 'no available LLM provider' error, got %v", err)
	}
}

type rateLimitMockProvider struct {
	name  string
	calls int
}

func (p *rateLimitMockProvider) Name() string {
	return p.name
}

func (p *rateLimitMockProvider) Available(ctx context.Context) bool {
	return true
}

func (p *rateLimitMockProvider) Complete(ctx context.Context, req Request) (<-chan Token, error) {
	p.calls++
	if p.calls <= 2 {
		return nil, errors.New("HTTP 429: Rate limit exceeded")
	}
	ch := make(chan Token)
	go func() {
		defer close(ch)
		ch <- Token{Text: "success"}
	}()
	return ch, nil
}

func (p *rateLimitMockProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, nil
}

func TestRouter_RateLimitRetry(t *testing.T) {
	viper.Set("llm.timeout_seconds", 60)
	defer viper.Reset()

	router := NewRouter()
	mock := &rateLimitMockProvider{name: "ratelimit"}
	router.Register(WorkloadHeal, mock)

	// Retry backoff is 2s + 4s = 6s total. Use 20s to avoid flakiness under
	// load (CI machines can stall, making 10s too tight).
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	ch, err := router.Complete(ctx, WorkloadHeal, Request{})
	if err != nil {
		t.Fatalf("expected success after retries, got err: %v", err)
	}

	token := <-ch
	if token.Text != "success" {
		t.Fatalf("expected 'success', got %v", token.Text)
	}
	if mock.calls != 3 {
		t.Fatalf("expected 3 calls (2 failures, 1 success), got %d", mock.calls)
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

func TestIsRL(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"HTTP 429 Too Many Requests", true},
		{"Rate limit exceeded for model", true},
		{"quota exceeded", true},
		{"too many requests", true},
		{"internal server error", false},
		{"500 server error", false},
	}

	for _, c := range cases {
		if got := isRL(c.msg); got != c.want {
			t.Errorf("isRL(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
}

func TestRouterContextWindow(t *testing.T) {
	r := NewRouter()
	ctx := context.Background()

	// Empty router defaults to 4096
	if got := r.ContextWindow(ctx, WorkloadDiagnose); got != 4096 {
		t.Fatalf("empty router ContextWindow = %d, want 4096", got)
	}

	// Register known provider
	r.Register(WorkloadDiagnose, &stubProvider{name: "gemini", available: true})
	if got := r.ContextWindow(ctx, WorkloadDiagnose); got != 1_000_000 {
		t.Fatalf("gemini ContextWindow = %d, want 1000000", got)
	}

	// Workload with Groq
	rGroq := NewRouter()
	rGroq.Register(WorkloadDiagnose, &stubProvider{name: "groq", available: true})
	if got := rGroq.ContextWindow(ctx, WorkloadDiagnose); got != 8_192 {
		t.Fatalf("groq ContextWindow = %d, want 8192", got)
	}
}

// chunkedProvider streams several tokens with a gap between them, so a stream
// that gets cut short by a cancellation is observable.
type chunkedProvider struct {
	name   string
	chunks []string
	gap    time.Duration

	calls atomic.Int32
}

func (p *chunkedProvider) Name() string                   { return p.name }
func (p *chunkedProvider) Available(context.Context) bool { return true }

func (p *chunkedProvider) Complete(ctx context.Context, _ Request) (<-chan Token, error) {
	p.calls.Add(1)
	ch := make(chan Token)
	go func() {
		defer close(ch)
		for i, c := range p.chunks {
			if i > 0 && p.gap > 0 {
				select {
				case <-time.After(p.gap):
				case <-ctx.Done():
					return
				}
			}
			select {
			case ch <- Token{Text: c, IsLast: i == len(p.chunks)-1}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (p *chunkedProvider) Embed(context.Context, string) ([]float32, error) {
	return nil, errors.New("unimplemented")
}

// fanoutTestConfig enables a 2-wide race with a generous per-attempt timeout.
func fanoutTestConfig(t *testing.T, width, timeoutSec int) {
	t.Helper()
	viper.Set("llm.parallel_fanout", width)
	viper.Set("llm.timeout_seconds", timeoutSec)
	t.Cleanup(viper.Reset)
}

func TestRouterParallelFastProviderWins(t *testing.T) {
	fanoutTestConfig(t, 2, 30)

	r := NewRouter()
	slow := &stubProvider{name: "slow", available: true, delay: 3 * time.Second, response: "slow-ok"}
	fast := &stubProvider{name: "fast", available: true, delay: 10 * time.Millisecond, response: "fast-ok"}
	r.Register(WorkloadHeal, slow)
	r.Register(WorkloadHeal, fast)

	start := time.Now()
	tokens, err := r.Complete(context.Background(), WorkloadHeal, Request{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := drainTokens(tokens); got != "fast-ok" {
		t.Fatalf("response = %q, want the fast provider to win the race", got)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("fan-out took %v, want it to return as soon as the fast provider answers", elapsed)
	}
	if slow.calls.Load() != 1 || fast.calls.Load() != 1 {
		t.Fatalf("provider calls slow=%d fast=%d, want both fired exactly once", slow.calls.Load(), fast.calls.Load())
	}
}

func TestRouterParallelUsesSlowProviderWhenFastErrors(t *testing.T) {
	fanoutTestConfig(t, 2, 30)

	r := NewRouter()
	fast := &stubProvider{name: "fast", available: true, err: errors.New("upstream boom")}
	slow := &stubProvider{name: "slow", available: true, delay: 150 * time.Millisecond, response: "slow-ok"}
	r.Register(WorkloadHeal, fast)
	r.Register(WorkloadHeal, slow)

	tokens, err := r.Complete(context.Background(), WorkloadHeal, Request{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := drainTokens(tokens); got != "slow-ok" {
		t.Fatalf("response = %q, want the slow provider's result after the fast one errored", got)
	}
	if fast.calls.Load() != 1 || slow.calls.Load() != 1 {
		t.Fatalf("provider calls fast=%d slow=%d, want both fired exactly once", fast.calls.Load(), slow.calls.Load())
	}
}

func TestRouterParallelSingleProviderUsesSequential(t *testing.T) {
	fanoutTestConfig(t, 3, 30)

	r := NewRouter()
	r.Register(WorkloadHeal, &stubProvider{name: "down", available: false})
	only := &stubProvider{name: "only", available: true, delay: 200 * time.Millisecond, response: "solo-ok"}
	r.Register(WorkloadHeal, only)

	start := time.Now()
	tokens, err := r.Complete(context.Background(), WorkloadHeal, Request{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := drainTokens(tokens); got != "solo-ok" {
		t.Fatalf("response = %q, want the only provider's response", got)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Fatalf("sequential path returned after %v, want it to wait for the only provider", elapsed)
	}
	if only.calls.Load() != 1 {
		t.Fatalf("only provider called %d times, want 1", only.calls.Load())
	}
}

func TestRouterParallelWidthOneStaysSequential(t *testing.T) {
	fanoutTestConfig(t, 1, 30)

	r := NewRouter()
	primary := &stubProvider{name: "primary", available: true, delay: 50 * time.Millisecond, response: "primary-ok"}
	secondary := &stubProvider{name: "secondary", available: true, response: "secondary-ok"}
	r.Register(WorkloadHeal, primary)
	r.Register(WorkloadHeal, secondary)

	tokens, err := r.Complete(context.Background(), WorkloadHeal, Request{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := drainTokens(tokens); got != "primary-ok" {
		t.Fatalf("response = %q, want the primary provider", got)
	}
	if secondary.calls.Load() != 0 {
		t.Fatalf("secondary provider called %d times, want 0 with fan-out disabled", secondary.calls.Load())
	}
}

func TestRouterParallelSkipsRateLimitedProvider(t *testing.T) {
	fanoutTestConfig(t, 2, 30)

	r := NewRouter()
	throttled := &stubProvider{name: "throttled", available: true, response: "throttled-ok"}
	healthy := &stubProvider{name: "healthy", available: true, response: "healthy-ok"}
	r.Register(WorkloadHeal, throttled)
	r.Register(WorkloadHeal, healthy)
	r.startCooldown("throttled", time.Minute)

	tokens, err := r.Complete(context.Background(), WorkloadHeal, Request{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := drainTokens(tokens); got != "healthy-ok" {
		t.Fatalf("response = %q, want the provider that is not rate limited", got)
	}
	if throttled.calls.Load() != 0 {
		t.Fatalf("rate-limited provider called %d times, want 0", throttled.calls.Load())
	}
	if healthy.calls.Load() != 1 {
		t.Fatalf("healthy provider called %d times, want 1", healthy.calls.Load())
	}
}

func TestRouterParallelKeepsRateLimitedProviderWhenItIsTheOnlyOne(t *testing.T) {
	fanoutTestConfig(t, 2, 30)

	r := NewRouter()
	only := &stubProvider{name: "only", available: true, response: "solo-ok"}
	r.Register(WorkloadHeal, only)
	r.startCooldown("only", time.Minute)

	tokens, err := r.Complete(context.Background(), WorkloadHeal, Request{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := drainTokens(tokens); got != "solo-ok" {
		t.Fatalf("response = %q, want the only provider to still be tried", got)
	}
	if only.calls.Load() != 1 {
		t.Fatalf("only provider called %d times, want 1", only.calls.Load())
	}
}

func TestRouterParallelWinnerStreamSurvivesHandoff(t *testing.T) {
	fanoutTestConfig(t, 2, 30)

	r := NewRouter()
	slow := &stubProvider{name: "slow", available: true, delay: 2 * time.Second, response: "slow-ok"}
	winner := &chunkedProvider{name: "winner", chunks: []string{"a", "b", "c"}, gap: 25 * time.Millisecond}
	r.Register(WorkloadHeal, slow)
	r.Register(WorkloadHeal, winner)

	tokens, err := r.Complete(context.Background(), WorkloadHeal, Request{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := drainTokens(tokens); got != "abc" {
		t.Fatalf("response = %q, want the full winning stream", got)
	}
	if slow.calls.Load() != 1 {
		t.Fatalf("slow provider called %d times, want it fired then cancelled", slow.calls.Load())
	}
}

func TestRouterParallelConcurrentComplete(t *testing.T) {
	fanoutTestConfig(t, 2, 30)

	r := NewRouter()
	fast := &stubProvider{name: "fast", available: true, delay: 10 * time.Millisecond, response: "fast-ok"}
	slow := &stubProvider{name: "slow", available: true, delay: 20 * time.Millisecond, response: "slow-ok"}
	r.Register(WorkloadHeal, fast)
	r.Register(WorkloadHeal, slow)

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tokens, err := r.Complete(context.Background(), WorkloadHeal, Request{})
			if err != nil {
				errs <- err
				return
			}
			if got := drainTokens(tokens); got != "fast-ok" {
				errs <- errors.New("unexpected response: " + got)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent fan-out Complete error: %v", err)
	}
	if fast.calls.Load() != 10 {
		t.Fatalf("fast provider called %d times, want 10", fast.calls.Load())
	}
}

func TestRoutingConfig(t *testing.T) {
	cases := []struct {
		fanout   int
		timeout  int
		wantWide int
		wantTo   time.Duration
	}{
		{0, 0, defaultParallelFanout, defaultTimeout},
		{1, 5, 1, 5 * time.Second},
		{2, 30, 2, 30 * time.Second},
		{3, 30, maxParallelFanout, 30 * time.Second},
		{9, 30, maxParallelFanout, 30 * time.Second},
		{-1, -1, defaultParallelFanout, defaultTimeout},
	}

	for _, c := range cases {
		viper.Set("llm.parallel_fanout", c.fanout)
		viper.Set("llm.timeout_seconds", c.timeout)
		got := routingConfig()
		if got.ParallelFanout != c.wantWide {
			t.Errorf("parallel_fanout=%d gave width %d, want %d", c.fanout, got.ParallelFanout, c.wantWide)
		}
		if got.Timeout != c.wantTo {
			t.Errorf("timeout_seconds=%d gave timeout %v, want %v", c.timeout, got.Timeout, c.wantTo)
		}
	}
	viper.Reset()
}
