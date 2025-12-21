package compact

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"text/template"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/steveyegge/beads/internal/types"
)

const (
	defaultAnthropicModel = "claude-3-5-haiku-20241022"
)

// AnthropicProvider implements the Provider interface using Anthropic's Claude API.
type AnthropicProvider struct {
	client         anthropic.Client
	model          anthropic.Model
	tier1Template  *template.Template
	maxRetries     int
	initialBackoff time.Duration
}

// NewAnthropicProvider creates a new Anthropic provider instance.
// The apiKey parameter is used if provided, otherwise ANTHROPIC_API_KEY env var is checked.
func NewAnthropicProvider(apiKey string, model string) (*AnthropicProvider, error) {
	// Environment variable takes precedence
	envKey := os.Getenv("ANTHROPIC_API_KEY")
	if envKey != "" {
		apiKey = envKey
	}
	if apiKey == "" {
		return nil, fmt.Errorf("%w: set ANTHROPIC_API_KEY environment variable or provide via config", ErrAPIKeyRequired)
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	tier1Tmpl, err := template.New("tier1").Parse(tier1PromptTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse tier1 template: %w", err)
	}

	// Use default model if not specified
	if model == "" {
		model = defaultAnthropicModel
	}

	return &AnthropicProvider{
		client:         client,
		model:          anthropic.Model(model),
		tier1Template:  tier1Tmpl,
		maxRetries:     maxRetries,
		initialBackoff: initialBackoff,
	}, nil
}

// Summarize generates a structured summary of an issue using Claude.
func (p *AnthropicProvider) Summarize(ctx context.Context, issue *types.Issue) (string, error) {
	prompt, err := p.renderTier1Prompt(issue)
	if err != nil {
		return "", fmt.Errorf("failed to render prompt: %w", err)
	}

	return p.callWithRetry(ctx, prompt)
}

func (p *AnthropicProvider) callWithRetry(ctx context.Context, prompt string) (string, error) {
	var lastErr error
	params := anthropic.MessageNewParams{
		Model:     p.model,
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	}

	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := p.initialBackoff * time.Duration(math.Pow(2, float64(attempt-1)))
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		message, err := p.client.Messages.New(ctx, params)

		if err == nil {
			if len(message.Content) > 0 {
				content := message.Content[0]
				if content.Type == "text" {
					return content.Text, nil
				}
				return "", fmt.Errorf("unexpected response format: not a text block (type=%s)", content.Type)
			}
			return "", fmt.Errorf("unexpected response format: no content blocks")
		}

		lastErr = err

		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		if !p.isRetryable(err) {
			return "", fmt.Errorf("non-retryable error: %w", err)
		}
	}

	return "", fmt.Errorf("failed after %d retries: %w", p.maxRetries+1, lastErr)
}

func (p *AnthropicProvider) isRetryable(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		statusCode := apiErr.StatusCode
		if statusCode == 429 || statusCode >= 500 {
			return true
		}
		return false
	}

	return false
}

func (p *AnthropicProvider) renderTier1Prompt(issue *types.Issue) (string, error) {
	var buf []byte
	w := &bytesWriter{buf: buf}

	data := tier1Data{
		Title:              issue.Title,
		Description:        issue.Description,
		Design:             issue.Design,
		AcceptanceCriteria: issue.AcceptanceCriteria,
		Notes:              issue.Notes,
	}

	if err := p.tier1Template.Execute(w, data); err != nil {
		return "", err
	}
	return string(w.buf), nil
}
