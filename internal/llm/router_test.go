package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

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

	// Since we are mocking time internally with time.After in router, it will take 2s + 4s = 6s.
	// We can't easily mock time, so we just let it run. It will be slightly slow.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
