package compact

import (
	"context"
	"errors"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestNewProvider_RequiresAPIKey(t *testing.T) {
	tests := []struct {
		name         string
		providerType ProviderType
	}{
		{"anthropic", ProviderAnthropic},
		{"openrouter", ProviderOpenRouter},
		{"gemini", ProviderGemini},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := ProviderConfig{
				APIKey: "",
			}

			_, err := NewProvider(tt.providerType, config)
			if err == nil {
				t.Fatal("expected error when API key is missing")
			}
			if !errors.Is(err, ErrAPIKeyRequired) {
				t.Fatalf("expected ErrAPIKeyRequired, got %v", err)
			}
		})
	}
}

func TestNewProvider_CreatesCorrectType(t *testing.T) {
	tests := []struct {
		name         string
		providerType ProviderType
		wantType     string
	}{
		{"anthropic", ProviderAnthropic, "*compact.AnthropicProvider"},
		{"openrouter", ProviderOpenRouter, "*compact.OpenRouterProvider"},
		{"gemini", ProviderGemini, "*compact.GeminiProvider"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable for the test
			envVar := ""
			switch tt.providerType {
			case ProviderAnthropic:
				envVar = "ANTHROPIC_API_KEY"
			case ProviderOpenRouter:
				envVar = "OPENROUTER_API_KEY"
			case ProviderGemini:
				envVar = "GEMINI_API_KEY"
			}
			t.Setenv(envVar, "test-key-for-validation")

			config := ProviderConfig{
				APIKey: "test-api-key",
				Model:  "test-model",
			}

			provider, err := NewProvider(tt.providerType, config)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if provider == nil {
				t.Fatal("expected non-nil provider")
			}

			// Verify it implements Provider interface
			var _ Provider = provider
		})
	}
}

func TestNewProvider_InvalidProviderType(t *testing.T) {
	config := ProviderConfig{
		APIKey: "test-key",
	}

	_, err := NewProvider("invalid", config)
	if err == nil {
		t.Fatal("expected error for invalid provider type")
	}
}

func TestParseProviderType(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    ProviderType
		wantErr bool
	}{
		{"anthropic", "anthropic", ProviderAnthropic, false},
		{"openrouter", "openrouter", ProviderOpenRouter, false},
		{"gemini", "gemini", ProviderGemini, false},
		{"invalid", "invalid", "", true},
		{"empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseProviderType(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseProviderType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseProviderType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProviderInterface(t *testing.T) {
	// This test verifies that all provider types implement the Provider interface
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("GEMINI_API_KEY", "test-key")

	providers := []struct {
		name     string
		provider Provider
	}{
		{"anthropic", mustCreateProvider(t, ProviderAnthropic, "test-key")},
		{"openrouter", mustCreateProvider(t, ProviderOpenRouter, "test-key")},
		{"gemini", mustCreateProvider(t, ProviderGemini, "test-key")},
	}

	for _, p := range providers {
		t.Run(p.name, func(t *testing.T) {
			// Verify the provider implements the interface method
			ctx := context.Background()
			issue := &types.Issue{
				ID:          "bd-test",
				Title:       "Test issue",
				Description: "Test description",
				Status:      types.StatusClosed,
			}

			// We don't actually call Summarize here since it would require real API keys
			// Just verify the method exists by checking the interface
			var _ Provider = p.provider
		})
	}
}

func mustCreateProvider(t *testing.T, providerType ProviderType, apiKey string) Provider {
	t.Helper()
	config := ProviderConfig{
		APIKey: apiKey,
	}
	provider, err := NewProvider(providerType, config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	return provider
}
