package providers

import (
	"context"
	"testing"
	"time"
)

func TestStreamingSupport(t *testing.T) {
	tests := []struct {
		name         string
		providerFunc func(t *testing.T) Provider
		model        string
	}{
		{
			name: "OpenAI Streaming",
			providerFunc: func(t *testing.T) Provider {
				p, _ := NewOpenAIProvider("test-openai", ProviderConfig{
					APIKey:  "test-key",
					BaseURL: "https://api.openai.com/v1",
					Models:  []string{"gpt-3.5-turbo"},
				})
				return p
			},
			model: "gpt-3.5-turbo",
		},
		{
			name: "Anthropic Streaming",
			providerFunc: func(t *testing.T) Provider {
				p, _ := NewAnthropicProvider("test-anthropic", ProviderConfig{
					APIKey:  "test-key",
					BaseURL: "https://api.anthropic.com",
					Models:  []string{"claude-3-haiku-20240307"},
				})
				return p
			},
			model: "claude-3-haiku-20240307",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := tt.providerFunc(t)
			if provider == nil {
				t.Skip("Provider initialization failed")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			request := &ChatRequest{
				Model: tt.model,
				Messages: []Message{
					{Role: "user", Content: "Test message"},
				},
				Stream: true,
			}

			streamChan, err := provider.ChatCompletionStream(ctx, request)
			if err != nil {
				t.Logf("ChatCompletionStream returned error (expected without real API): %v", err)
			}

			if streamChan == nil {
				t.Errorf("%s: ChatCompletionStream should return a channel, got nil", tt.name)
			}

			select {
			case <-streamChan:
			case <-time.After(200 * time.Millisecond):
			}
		})
	}
}

func TestStreamingChannelBehavior(t *testing.T) {
	providers := []struct {
		name     string
		provider Provider
	}{
		{
			name: "OpenAI",
			provider: func() Provider {
				p, _ := NewOpenAIProvider("test", ProviderConfig{
					APIKey: "test-key",
				})
				return p
			}(),
		},
		{
			name: "Anthropic",
			provider: func() Provider {
				p, _ := NewAnthropicProvider("test", ProviderConfig{
					APIKey: "test-key",
				})
				return p
			}(),
		},
	}

	for _, p := range providers {
		t.Run(p.name+" channel creation", func(t *testing.T) {
			if p.provider == nil {
				t.Skip("Provider initialization failed")
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			request := &ChatRequest{
				Model: "test-model",
				Messages: []Message{
					{Role: "user", Content: "test"},
				},
				Stream: true,
			}

			streamChan, _ := p.provider.ChatCompletionStream(ctx, request)

			if streamChan != nil {
				cancel()

				timer := time.NewTimer(500 * time.Millisecond)
				defer timer.Stop()

				select {
				case <-streamChan:
				case <-timer.C:
				}
			}
		})
	}
}
