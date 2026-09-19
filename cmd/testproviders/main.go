package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/zalando/go-keyring"
)

type providerSpec struct {
	name       string
	keyringKey string
	baseURL    string
	primary    []string
	alts       []string
}

var providers = []providerSpec{
	{
		name:       "GROQ",
		keyringKey: "groq_api_key",
		baseURL:    "https://api.groq.com/openai/v1",
		primary:    []string{"groq/compound-mini", "groq/compound"},
		alts:       []string{"llama-3.1-8b-instant", "llama-3.3-70b-versatile", "gemma2-9b-it"},
	},
	{
		name:       "MISTRAL",
		keyringKey: "mistral_api_key",
		baseURL:    "https://api.mistral.ai/v1",
		primary:    []string{"open-mistral-7b"},
		alts:       []string{"mistral-small-latest", "open-mixtral-8x7b", "mistral-medium-latest"},
	},
	{
		name:       "NVIDIA",
		keyringKey: "nvidia_api_key",
		baseURL:    "https://integrate.api.nvidia.com/v1",
		primary:    []string{"nvidia/nemotron-3-super-120b-a12b"},
		alts:       []string{"meta/llama-3.1-8b-instruct", "meta/llama-3.3-70b-instruct", "mistralai/mistral-7b-instruct-v0.3", "google/gemma-3-27b-it"},
	},
}

func testModel(client openai.Client, provider, model string) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModel(model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("Say the word OK and nothing else"),
		},
		MaxTokens: openai.Int(10),
	})
	if err != nil {
		msg := err.Error()
		if len(msg) > 120 {
			msg = msg[:120] + "..."
		}
		return false, msg
	}
	if len(resp.Choices) == 0 {
		return false, "empty choices"
	}
	reply := strings.TrimSpace(resp.Choices[0].Message.Content)
	if len(reply) > 40 {
		reply = reply[:40]
	}
	return true, reply
}

func main() {
	fmt.Println("=== Nebula Provider Model Test ===")
	fmt.Println()

	for _, p := range providers {
		apiKey, err := keyring.Get("nebula", p.keyringKey)
		if err != nil || apiKey == "" {
			fmt.Printf("[%s] no API key in keyring (%v), skipping\n\n", p.name, err)
			continue
		}
		fmt.Printf("[%s] key found (%s...)\n", p.name, apiKey[:8])

		client := openai.NewClient(
			option.WithAPIKey(apiKey),
			option.WithBaseURL(p.baseURL),
		)

		anyFailed := false
		for _, model := range p.primary {
			ok, msg := testModel(client, p.name, model)
			if ok {
				fmt.Printf("  %-55s OK (%q)\n", model, msg)
			} else {
				fmt.Printf("  %-55s FAIL (%s)\n", model, msg)
				anyFailed = true
			}
		}

		if anyFailed {
			fmt.Printf("  -- trying alternatives --\n")
			for _, model := range p.alts {
				ok, msg := testModel(client, p.name, model)
				if ok {
					fmt.Printf("  %-55s OK (%q)\n", model, msg)
				} else {
					fmt.Printf("  %-55s FAIL (%s)\n", model, msg)
				}
			}
		}
		fmt.Println()
	}
}
