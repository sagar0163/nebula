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
