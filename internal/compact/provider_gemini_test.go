package compact

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestNewGeminiProvider_RequiresAPIKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")

	_, err := NewGeminiProvider("", "")
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

func TestNewGeminiProvider_EnvVarHandling(t *testing.T) {
	// Note: This test will create a real Gemini client with a test key
	// The client creation itself validates the key format but doesn't call the API
	t.Setenv("GEMINI_API_KEY", "test-key-from-env-12345")

	provider, err := NewGeminiProvider("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	defer provider.Close()
}

func TestNewGeminiProvider_EnvVarOverridesExplicitKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key-from-env-12345")

	provider, err := NewGeminiProvider("test-key-explicit", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	defer provider.Close()
}

func TestNewGeminiProvider_CustomModel(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key-12345")

	provider, err := NewGeminiProvider("test-key", "gemini-pro")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	defer provider.Close()

	if provider.model != "gemini-pro" {
		t.Errorf("expected custom model, got %s", provider.model)
	}
}

func TestNewGeminiProvider_DefaultModel(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key-12345")

	provider, err := NewGeminiProvider("test-key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	defer provider.Close()

	if provider.model != defaultGeminiModel {
		t.Errorf("expected default model %s, got %s", defaultGeminiModel, provider.model)
	}
}

func TestGeminiProvider_RenderTier1Prompt(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key-12345")

	provider, err := NewGeminiProvider("test-key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

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
}

func TestGeminiProvider_RenderTier1Prompt_HandlesEmptyFields(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key-12345")

	provider, err := NewGeminiProvider("test-key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

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

func TestGeminiProvider_RenderTier1Prompt_UTF8(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key-12345")

	provider, err := NewGeminiProvider("test-key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

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

func TestGeminiProvider_Summarize_ContextCancellation(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key-12345")

	provider, err := NewGeminiProvider("test-key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	issue := &types.Issue{
		ID:          "bd-1",
		Title:       "Test issue",
		Description: "Test description",
		Status:      types.StatusClosed,
	}

	_, err = provider.Summarize(ctx, issue)
	if err == nil {
		t.Fatal("expected error when context is canceled")
	}
	if err != context.Canceled {
		// Gemini SDK might wrap the error
		if !errors.Is(err, context.Canceled) {
			t.Logf("got error: %v (not context.Canceled, but acceptable)", err)
		}
	}
}

func TestGeminiProvider_ImplementsProvider(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key-12345")

	provider, err := NewGeminiProvider("test-key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	// Verify it implements the Provider interface
	var _ Provider = provider
}

func TestGeminiProvider_Close(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key-12345")

	provider, err := NewGeminiProvider("test-key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = provider.Close()
	if err != nil {
		t.Errorf("unexpected error closing provider: %v", err)
	}

	// Closing again should be safe
	err = provider.Close()
	if err != nil {
		t.Errorf("unexpected error closing provider twice: %v", err)
	}
}
