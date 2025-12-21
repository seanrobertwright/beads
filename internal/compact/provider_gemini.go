package compact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"text/template"

	"github.com/google/generative-ai-go/genai"
	"github.com/steveyegge/beads/internal/types"
	"google.golang.org/api/option"
)

const (
	defaultGeminiModel = "gemini-2.0-flash-exp"
)

// GeminiProvider implements the Provider interface using Google's Gemini API.
type GeminiProvider struct {
	client        *genai.Client
	model         string
	tier1Template *template.Template
}

// NewGeminiProvider creates a new Gemini provider instance.
func NewGeminiProvider(apiKey string, model string) (*GeminiProvider, error) {
	// Environment variable takes precedence
	envKey := os.Getenv("GEMINI_API_KEY")
	if envKey != "" {
		apiKey = envKey
	}
	if apiKey == "" {
		return nil, fmt.Errorf("%w: set GEMINI_API_KEY environment variable or provide via config", ErrAPIKeyRequired)
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	tier1Tmpl, err := template.New("tier1").Parse(tier1PromptTemplate)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to parse tier1 template: %w", err)
	}

	// Use default model if not specified
	if model == "" {
		model = defaultGeminiModel
	}

	return &GeminiProvider{
		client:        client,
		model:         model,
		tier1Template: tier1Tmpl,
	}, nil
}

// Summarize generates a structured summary of an issue using Gemini.
func (p *GeminiProvider) Summarize(ctx context.Context, issue *types.Issue) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	prompt, err := p.renderTier1Prompt(issue)
	if err != nil {
		return "", fmt.Errorf("failed to render prompt: %w", err)
	}

	model := p.client.GenerativeModel(p.model)

	// Configure the model for consistent output
	model.SetMaxOutputTokens(1024)
	model.SetTemperature(0.7)

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return "", fmt.Errorf("failed to generate content: %w", err)
	}

	if resp == nil || len(resp.Candidates) == 0 {
		return "", errors.New("no response from Gemini API")
	}

	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return "", errors.New("empty response from Gemini API")
	}

	// Extract text from the first part
	part := candidate.Content.Parts[0]
	text, ok := part.(genai.Text)
	if !ok {
		return "", fmt.Errorf("unexpected response type: %T", part)
	}

	return string(text), nil
}

func (p *GeminiProvider) renderTier1Prompt(issue *types.Issue) (string, error) {
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

// Close closes the Gemini client connection.
func (p *GeminiProvider) Close() error {
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}
