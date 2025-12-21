package compact

import (
	"context"
	"errors"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

// ProviderType represents the type of LLM provider.
type ProviderType string

const (
	// ProviderAnthropic uses Anthropic Claude models.
	ProviderAnthropic ProviderType = "anthropic"
	// ProviderOpenRouter uses OpenRouter API gateway.
	ProviderOpenRouter ProviderType = "openrouter"
	// ProviderGemini uses Google Gemini models.
	ProviderGemini ProviderType = "gemini"
)

// Provider is the interface that all LLM providers must implement.
type Provider interface {
	// Summarize generates a structured summary of an issue.
	Summarize(ctx context.Context, issue *types.Issue) (string, error)
}

// ProviderConfig holds configuration for creating a provider.
type ProviderConfig struct {
	// APIKey is the authentication key for the provider.
	APIKey string
	// Model is the specific model to use (optional, provider-specific default used if empty).
	Model string
}

// NewProvider creates a new provider instance based on the provider type.
func NewProvider(providerType ProviderType, config ProviderConfig) (Provider, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("%w: %s provider requires API key", ErrAPIKeyRequired, providerType)
	}

	switch providerType {
	case ProviderAnthropic:
		return NewAnthropicProvider(config.APIKey, config.Model)
	case ProviderOpenRouter:
		return NewOpenRouterProvider(config.APIKey, config.Model)
	case ProviderGemini:
		return NewGeminiProvider(config.APIKey, config.Model)
	default:
		return nil, fmt.Errorf("unknown provider: %s (valid options: anthropic, openrouter, gemini)", providerType)
	}
}

// ParseProviderType converts a string to a ProviderType.
func ParseProviderType(s string) (ProviderType, error) {
	switch s {
	case "anthropic":
		return ProviderAnthropic, nil
	case "openrouter":
		return ProviderOpenRouter, nil
	case "gemini":
		return ProviderGemini, nil
	default:
		return "", fmt.Errorf("unknown provider: %s (valid options: anthropic, openrouter, gemini)", s)
	}
}
