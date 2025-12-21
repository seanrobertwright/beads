package compact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"text/template"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

const (
	defaultOpenRouterModel = "anthropic/claude-3-5-haiku"
	openRouterAPIURL       = "https://openrouter.ai/api/v1/chat/completions"
)

// OpenRouterProvider implements the Provider interface using OpenRouter's API.
type OpenRouterProvider struct {
	client         *http.Client
	apiKey         string
	model          string
	tier1Template  *template.Template
	maxRetries     int
	initialBackoff time.Duration
}

// openRouterRequest represents the request format for OpenRouter API.
type openRouterRequest struct {
	Model    string                   `json:"model"`
	Messages []openRouterMessage      `json:"messages"`
	MaxTokens int                     `json:"max_tokens,omitempty"`
}

// openRouterMessage represents a message in the OpenRouter API format.
type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openRouterResponse represents the response format from OpenRouter API.
type openRouterResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []openRouterChoice     `json:"choices"`
	Usage   map[string]interface{} `json:"usage,omitempty"`
	Error   *openRouterError       `json:"error,omitempty"`
}

// openRouterChoice represents a choice in the response.
type openRouterChoice struct {
	Index        int                `json:"index"`
	Message      openRouterMessage  `json:"message"`
	FinishReason string             `json:"finish_reason"`
}

// openRouterError represents an error response from OpenRouter.
type openRouterError struct {
	Message string                 `json:"message"`
	Type    string                 `json:"type"`
	Code    string                 `json:"code"`
}

// NewOpenRouterProvider creates a new OpenRouter provider instance.
func NewOpenRouterProvider(apiKey string, model string) (*OpenRouterProvider, error) {
	// Environment variable takes precedence
	envKey := os.Getenv("OPENROUTER_API_KEY")
	if envKey != "" {
		apiKey = envKey
	}
	if apiKey == "" {
		return nil, fmt.Errorf("%w: set OPENROUTER_API_KEY environment variable or provide via config", ErrAPIKeyRequired)
	}

	tier1Tmpl, err := template.New("tier1").Parse(tier1PromptTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse tier1 template: %w", err)
	}

	// Use default model if not specified
	if model == "" {
		model = defaultOpenRouterModel
	}

	return &OpenRouterProvider{
		client:         &http.Client{Timeout: 60 * time.Second},
		apiKey:         apiKey,
		model:          model,
		tier1Template:  tier1Tmpl,
		maxRetries:     maxRetries,
		initialBackoff: initialBackoff,
	}, nil
}

// Summarize generates a structured summary of an issue using OpenRouter.
func (p *OpenRouterProvider) Summarize(ctx context.Context, issue *types.Issue) (string, error) {
	prompt, err := p.renderTier1Prompt(issue)
	if err != nil {
		return "", fmt.Errorf("failed to render prompt: %w", err)
	}

	return p.callWithRetry(ctx, prompt)
}

func (p *OpenRouterProvider) callWithRetry(ctx context.Context, prompt string) (string, error) {
	var lastErr error

	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := p.initialBackoff * time.Duration(math.Pow(2, float64(attempt-1)))
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		result, err := p.makeAPICall(ctx, prompt)
		if err == nil {
			return result, nil
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

func (p *OpenRouterProvider) makeAPICall(ctx context.Context, prompt string) (string, error) {
	reqBody := openRouterRequest{
		Model: p.model,
		Messages: []openRouterMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		MaxTokens: 1024,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", openRouterAPIURL, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/steveyegge/beads")
	req.Header.Set("X-Title", "beads issue tracker")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp openRouterResponse
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != nil {
			return "", &httpError{
				statusCode: resp.StatusCode,
				message:    errResp.Error.Message,
			}
		}
		return "", &httpError{
			statusCode: resp.StatusCode,
			message:    string(body),
		}
	}

	var response openRouterResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("unexpected response format: no choices")
	}

	return response.Choices[0].Message.Content, nil
}

func (p *OpenRouterProvider) isRetryable(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var httpErr *httpError
	if errors.As(err, &httpErr) {
		// Retry on rate limit (429) and server errors (5xx)
		if httpErr.statusCode == 429 || httpErr.statusCode >= 500 {
			return true
		}
		return false
	}

	return false
}

func (p *OpenRouterProvider) renderTier1Prompt(issue *types.Issue) (string, error) {
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

// httpError represents an HTTP error with status code.
type httpError struct {
	statusCode int
	message    string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.statusCode, e.message)
}
