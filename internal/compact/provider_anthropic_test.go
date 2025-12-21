package compact

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestNewAnthropicProvider_RequiresAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	_, err := NewAnthropicProvider("", "")
	if err == nil {
		t.Fatal("expected error when API key is missing")
	}
	if !errors.Is(err, ErrAPIKeyRequired) {
		t.Fatalf("expected ErrAPIKeyRequired, got %v", err)
	}
	if !strings.Contains(err.Error(), "API key required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewAnthropicProvider_EnvVarUsedWhenNoExplicitKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-from-env")

	provider, err := NewAnthropicProvider("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewAnthropicProvider_EnvVarOverridesExplicitKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-from-env")

	provider, err := NewAnthropicProvider("test-key-explicit", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewAnthropicProvider_CustomModel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	provider, err := NewAnthropicProvider("test-key", "claude-3-opus-20240229")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if string(provider.model) != "claude-3-opus-20240229" {
		t.Errorf("expected custom model, got %s", provider.model)
	}
}

func TestNewAnthropicProvider_DefaultModel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	provider, err := NewAnthropicProvider("test-key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if string(provider.model) != defaultAnthropicModel {
		t.Errorf("expected default model %s, got %s", defaultAnthropicModel, provider.model)
	}
}

func TestAnthropicProvider_RenderTier1Prompt(t *testing.T) {
	provider, err := NewAnthropicProvider("test-key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	issue := &types.Issue{
		ID:                 "bd-1",
		Title:              "Fix authentication bug",
		Description:        "Users can't log in with OAuth",
		Design:             "Add error handling to OAuth flow",
		AcceptanceCriteria: "Users can log in successfully",
		Notes:              "Related to issue bd-2",
		Status:             types.StatusClosed,
	}

	prompt, err := provider.renderTier1Prompt(issue)
	if err != nil {
		t.Fatalf("failed to render prompt: %v", err)
	}

	if !strings.Contains(prompt, "Fix authentication bug") {
		t.Error("prompt should contain title")
	}
	if !strings.Contains(prompt, "Users can't log in with OAuth") {
		t.Error("prompt should contain description")
	}
	if !strings.Contains(prompt, "Add error handling to OAuth flow") {
		t.Error("prompt should contain design")
	}
	if !strings.Contains(prompt, "Users can log in successfully") {
		t.Error("prompt should contain acceptance criteria")
	}
	if !strings.Contains(prompt, "Related to issue bd-2") {
		t.Error("prompt should contain notes")
	}
	if !strings.Contains(prompt, "**Summary:**") {
		t.Error("prompt should contain format instructions")
	}
}

func TestAnthropicProvider_RenderTier1Prompt_HandlesEmptyFields(t *testing.T) {
	provider, err := NewAnthropicProvider("test-key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	issue := &types.Issue{
		ID:          "bd-1",
		Title:       "Simple task",
		Description: "Just a simple task",
		Status:      types.StatusClosed,
	}

	prompt, err := provider.renderTier1Prompt(issue)
	if err != nil {
		t.Fatalf("failed to render prompt: %v", err)
	}

	if !strings.Contains(prompt, "Simple task") {
		t.Error("prompt should contain title")
	}
	if !strings.Contains(prompt, "Just a simple task") {
		t.Error("prompt should contain description")
	}
}

func TestAnthropicProvider_RenderTier1Prompt_UTF8(t *testing.T) {
	provider, err := NewAnthropicProvider("test-key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	issue := &types.Issue{
		ID:          "bd-1",
		Title:       "Fix bug with émojis 🎉",
		Description: "Handle UTF-8: café, 日本語, emoji 🚀",
		Status:      types.StatusClosed,
	}

	prompt, err := provider.renderTier1Prompt(issue)
	if err != nil {
		t.Fatalf("failed to render prompt: %v", err)
	}

	if !strings.Contains(prompt, "🎉") {
		t.Error("prompt should preserve emoji in title")
	}
	if !strings.Contains(prompt, "café") {
		t.Error("prompt should preserve accented characters")
	}
	if !strings.Contains(prompt, "日本語") {
		t.Error("prompt should preserve unicode characters")
	}
	if !strings.Contains(prompt, "🚀") {
		t.Error("prompt should preserve emoji in description")
	}
}

func TestAnthropicProvider_CallWithRetry_ContextCancellation(t *testing.T) {
	provider, err := NewAnthropicProvider("test-key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	provider.initialBackoff = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = provider.callWithRetry(ctx, "test prompt")
	if err == nil {
		t.Fatal("expected error when context is canceled")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

func TestAnthropicProvider_IsRetryable(t *testing.T) {
	provider := &AnthropicProvider{}

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"context canceled", context.Canceled, false},
		{"context deadline exceeded", context.DeadlineExceeded, false},
		{"generic error", errors.New("some error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := provider.isRetryable(tt.err)
			if got != tt.expected {
				t.Errorf("isRetryable(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestAnthropicProvider_ImplementsProvider(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	provider, err := NewAnthropicProvider("test-key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it implements the Provider interface
	var _ Provider = provider
}
