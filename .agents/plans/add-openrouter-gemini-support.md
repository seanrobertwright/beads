# Feature: OpenRouter and Gemini LLM Provider Support

The following plan should be complete, but it's important that you validate documentation and codebase patterns and task sanity before you start implementing.

Pay special attention to naming of existing utils types and models. Import from the right files etc.

## Feature Description

Add support for OpenRouter and Google Gemini as alternative LLM providers for the `bd compact --auto` command. Currently, only Anthropic Claude (Haiku) is supported through direct API integration. This feature enables users to choose between multiple LLM providers based on cost, performance, or availability preferences.

OpenRouter provides a unified API gateway to hundreds of AI models (including GPT-4, Claude, Llama, etc.) with automatic fallbacks and cost optimization. Gemini offers Google's latest generative AI models with competitive pricing and performance.

## User Story

As a beads user
I want to choose between multiple LLM providers (Anthropic, OpenRouter, Gemini) for AI-powered issue compaction
So that I can optimize for cost, performance, or availability based on my specific needs and avoid vendor lock-in

## Problem Statement

The current implementation tightly couples the compaction feature to Anthropic's Claude API. Users who:
- Already have OpenRouter subscriptions (with access to multiple models)
- Prefer Google Gemini for cost or performance reasons
- Want to avoid dependency on a single LLM provider
- Need fallback options when primary provider is unavailable

...cannot use the `bd compact --auto` command without an Anthropic API key.

## Solution Statement

Implement a provider abstraction layer with three concrete implementations:
1. **Anthropic provider** (existing, refactored to use common interface)
2. **OpenRouter provider** (new, OpenAI-compatible API)
3. **Gemini provider** (new, using official Google GenAI SDK)

Add configuration options to select provider via environment variables or config file. Maintain backward compatibility with existing `ANTHROPIC_API_KEY` usage while supporting new providers through `BEADS_LLM_PROVIDER` and provider-specific API key variables.

## Feature Metadata

**Feature Type**: Enhancement
**Estimated Complexity**: Medium
**Primary Systems Affected**:
- `internal/compact/` (compactor and provider abstraction)
- `cmd/bd/compact.go` (command configuration)
- `internal/config/` (provider configuration)

**Dependencies**:
- Existing: `github.com/anthropics/anthropic-sdk-go v1.19.0`
- New: `google.golang.org/genai` (Gemini SDK)
- New: Standard library `net/http` (OpenRouter HTTP client)

---

## CONTEXT REFERENCES

### Relevant Codebase Files IMPORTANT: YOU MUST READ THESE FILES BEFORE IMPLEMENTING!

- `internal/compact/haiku.go` (lines 1-209) - Why: Existing Anthropic client implementation showing retry logic, error handling, prompt template patterns
- `internal/compact/haiku_test.go` (lines 1-192) - Why: Test patterns for API client validation, environment variable handling, prompt rendering
- `internal/compact/compactor.go` - Why: Compactor orchestration layer that will need to support multiple providers
- `internal/config/config.go` (lines 1-210) - Why: Configuration system using Viper with environment variable binding patterns
- `cmd/bd/compact.go` (lines 1-1167) - Why: Command implementation showing how API keys are currently validated and passed to compactor
- `go.mod` (lines 1-56) - Why: Dependency management - need to add new SDK packages here

### New Files to Create

- `internal/compact/provider.go` - Provider interface and factory for LLM client abstraction
- `internal/compact/provider_anthropic.go` - Refactored Anthropic provider (move from haiku.go)
- `internal/compact/provider_openrouter.go` - OpenRouter HTTP client implementation
- `internal/compact/provider_gemini.go` - Google Gemini SDK client implementation
- `internal/compact/provider_test.go` - Unit tests for provider factory and interface compliance
- `internal/compact/provider_openrouter_test.go` - OpenRouter client tests
- `internal/compact/provider_gemini_test.go` - Gemini client tests

### Relevant Documentation YOU SHOULD READ THESE BEFORE IMPLEMENTING!

- [OpenRouter API Reference](https://openrouter.ai/docs/api/reference/overview)
  - Specific section: Authentication and chat completions endpoint
  - Why: Required for implementing OpenRouter HTTP client with Bearer token auth and request/response format
- [OpenRouter API Parameters](https://openrouter.ai/docs/api/reference/parameters)
  - Specific section: Model selection and message format
  - Why: Shows model naming convention (e.g., "anthropic/claude-3-5-haiku") and parameter structure
- [Gemini API Documentation](https://ai.google.dev/gemini-api/docs)
  - Specific section: REST API and Go SDK usage
  - Why: Official Google documentation for Gemini integration patterns
- [Gemini API Quickstart](https://ai.google.dev/gemini-api/docs/quickstart)
  - Specific section: Go SDK installation and authentication
  - Why: Shows `genai.NewClient(ctx, nil)` pattern and `GEMINI_API_KEY` environment variable usage

### Patterns to Follow

**Naming Conventions:**
```go
// Package-level types use PascalCase
type Provider interface { ... }
type AnthropicProvider struct { ... }

// Configuration keys use kebab-case
"llm.provider"
"llm.anthropic.api-key"

// Environment variables use SCREAMING_SNAKE_CASE with prefix
BEADS_LLM_PROVIDER
ANTHROPIC_API_KEY  // Existing, keep for backward compat
OPENROUTER_API_KEY
GEMINI_API_KEY
```

**Error Handling Pattern (from haiku.go:120-144):**
```go
// Distinguish retryable vs non-retryable errors
func isRetryable(err error) bool {
    if err == nil {
        return false
    }

    // Never retry context cancellation
    if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
        return false
    }

    // Retry network timeouts
    var netErr net.Error
    if errors.As(err, &netErr) && netErr.Timeout() {
        return true
    }

    // Retry 429 (rate limit) and 5xx (server errors)
    // Don't retry 4xx (client errors) except 429
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
```

**Retry Logic Pattern (from haiku.go:73-118):**
```go
// Exponential backoff with max retries
const (
    maxRetries     = 3
    initialBackoff = 1 * time.Second
)

for attempt := 0; attempt <= maxRetries; attempt++ {
    if attempt > 0 {
        backoff := initialBackoff * time.Duration(math.Pow(2, float64(attempt-1)))
        select {
        case <-time.After(backoff):
        case <-ctx.Done():
            return "", ctx.Err()
        }
    }

    // Make API call...

    if err == nil {
        return result, nil
    }

    lastErr = err

    if ctx.Err() != nil {
        return "", ctx.Err()
    }

    if !isRetryable(err) {
        return "", fmt.Errorf("non-retryable error: %w", err)
    }
}

return "", fmt.Errorf("failed after %d retries: %w", maxRetries+1, lastErr)
```

**Configuration Pattern (from config.go:66-96):**
```go
// Environment variable binding with prefix and key replacement
v.SetEnvPrefix("BD")
v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
v.AutomaticEnv()

// Set defaults before reading config file
v.SetDefault("llm.provider", "anthropic")
v.SetDefault("llm.anthropic.model", "claude-3-5-haiku-20241022")
v.SetDefault("llm.openrouter.model", "anthropic/claude-3-5-haiku")
v.SetDefault("llm.gemini.model", "gemini-2.5-flash")

// Explicit binding for non-BD prefixed env vars (backward compat)
_ = v.BindEnv("llm.anthropic.api-key", "ANTHROPIC_API_KEY")
_ = v.BindEnv("llm.openrouter.api-key", "OPENROUTER_API_KEY")
_ = v.BindEnv("llm.gemini.api-key", "GEMINI_API_KEY")
```

**Template Pattern (from haiku.go:146-180):**
```go
// Use text/template for prompt construction
type tier1Data struct {
    Title              string
    Description        string
    Design             string
    AcceptanceCriteria string
    Notes              string
}

tier1Tmpl, err := template.New("tier1").Parse(tier1PromptTemplate)
if err != nil {
    return nil, fmt.Errorf("failed to parse tier1 template: %w", err)
}

// Render template to string
var buf []byte
w := &bytesWriter{buf: buf}
if err := tier1Template.Execute(w, data); err != nil {
    return "", err
}
return string(w.buf), nil
```

---

## IMPLEMENTATION PLAN

### Phase 1: Foundation - Provider Abstraction

Create the provider interface and factory pattern to decouple compaction logic from specific LLM implementations.

**Tasks:**
- Define `Provider` interface with `Summarize(ctx, issue) (string, error)` method
- Implement provider factory with `NewProvider(providerType, config)` constructor
- Add configuration structures for provider selection and API keys
- Update `internal/config/config.go` to support new LLM configuration keys

### Phase 2: Refactor Existing Anthropic Implementation

Move existing Haiku client into the new provider architecture without changing functionality.

**Tasks:**
- Create `provider_anthropic.go` with `AnthropicProvider` struct implementing `Provider` interface
- Move logic from `haiku.go` into new Anthropic provider
- Update tests to work with new provider structure
- Ensure backward compatibility with `ANTHROPIC_API_KEY` environment variable

### Phase 3: OpenRouter Implementation

Add OpenRouter support using HTTP client (no official Go SDK exists).

**Tasks:**
- Implement `OpenRouterProvider` with HTTP client and Bearer token authentication
- Add retry logic matching Anthropic pattern (3 retries, exponential backoff)
- Handle OpenAI-compatible request/response format
- Write tests with mocked HTTP responses

### Phase 4: Gemini Implementation

Add Google Gemini support using official Go SDK.

**Tasks:**
- Add `google.golang.org/genai` dependency to `go.mod`
- Implement `GeminiProvider` using Gemini SDK client
- Configure `GEMINI_API_KEY` environment variable handling
- Handle Gemini-specific response format and error types
- Write tests for Gemini client initialization and API calls

### Phase 5: Integration with Compactor

Wire up provider selection in the compactor and command layer.

**Tasks:**
- Update `compact.New()` to accept provider type and create appropriate provider
- Modify `cmd/bd/compact.go` to read provider configuration
- Add `--provider` flag to override default provider
- Update compactor to use `Provider` interface instead of direct Haiku client
- Ensure all compaction modes (auto, analyze, apply) work with new providers

### Phase 6: Testing & Validation

Comprehensive testing across all providers and edge cases.

**Tasks:**
- Test provider factory with all three provider types
- Verify retry logic works for each provider's error types
- Test API key validation for each provider
- Test provider selection via config file and environment variables
- Integration test for end-to-end compaction with each provider

---

## STEP-BY-STEP TASKS

IMPORTANT: Execute every task in order, top to bottom. Each task is atomic and independently testable.

### CREATE internal/compact/provider.go

- **IMPLEMENT**: Provider interface with Summarize method signature
  ```go
  type Provider interface {
      Summarize(ctx context.Context, issue *types.Issue) (string, error)
  }
  ```
- **IMPLEMENT**: ProviderConfig struct to hold API keys and model names
- **IMPLEMENT**: ProviderType enum (Anthropic, OpenRouter, Gemini)
- **IMPLEMENT**: NewProvider factory function with switch on provider type
- **PATTERN**: Error handling from haiku.go:25-26 (define ErrAPIKeyRequired)
- **IMPORTS**:
  ```go
  import (
      "context"
      "errors"
      "fmt"
      "github.com/steveyegge/beads/internal/types"
  )
  ```
- **GOTCHA**: Factory must validate API key is non-empty before creating provider
- **VALIDATE**: `go test -short ./internal/compact -run TestNewProvider`

### CREATE internal/compact/provider_test.go

- **IMPLEMENT**: TestNewProvider validates factory creates correct provider types
- **IMPLEMENT**: TestNewProvider_RequiresAPIKey validates error when API key missing
- **IMPLEMENT**: TestProviderInterface ensures all providers implement interface
- **PATTERN**: Test structure from haiku_test.go:13-26 (use t.Setenv for env vars)
- **IMPORTS**:
  ```go
  import (
      "context"
      "errors"
      "testing"
      "github.com/steveyegge/beads/internal/types"
  )
  ```
- **VALIDATE**: `go test -short ./internal/compact -run TestProvider`

### CREATE internal/compact/provider_anthropic.go

- **IMPLEMENT**: AnthropicProvider struct (move from HaikuClient)
- **IMPLEMENT**: NewAnthropicProvider constructor matching NewHaikuClient pattern
- **IMPLEMENT**: Summarize method wrapping existing SummarizeTier1 logic
- **MIRROR**: All retry logic from haiku.go:73-118 (callWithRetry method)
- **MIRROR**: Error handling from haiku.go:120-144 (isRetryable function)
- **MIRROR**: Template rendering from haiku.go:154-209
- **PATTERN**: Environment variable precedence from haiku.go:38-45
- **IMPORTS**:
  ```go
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
  ```
- **GOTCHA**: Keep tier1PromptTemplate constant identical to preserve backward compatibility
- **VALIDATE**: `go test -short ./internal/compact -run TestAnthropicProvider`

### CREATE internal/compact/provider_anthropic_test.go

- **IMPLEMENT**: Move all tests from haiku_test.go to new file
- **IMPLEMENT**: Update tests to use AnthropicProvider instead of HaikuClient
- **IMPLEMENT**: Test Summarize method instead of SummarizeTier1
- **PATTERN**: Existing test patterns from haiku_test.go:1-192
- **VALIDATE**: `go test -short ./internal/compact -run TestAnthropic`

### CREATE internal/compact/provider_openrouter.go

- **IMPLEMENT**: OpenRouterProvider struct with http.Client field
- **IMPLEMENT**: NewOpenRouterProvider constructor with API key validation
- **IMPLEMENT**: Summarize method making POST to https://openrouter.ai/api/v1/chat/completions
- **IMPLEMENT**: Request body with messages array and model parameter
- **IMPLEMENT**: Response parsing for OpenAI-compatible format (choices[0].message.content)
- **IMPLEMENT**: callWithRetry method matching Anthropic pattern (3 retries, exponential backoff)
- **IMPLEMENT**: isRetryable for HTTP status codes (429, 5xx retryable; 4xx not retryable except 429)
- **PATTERN**: Retry logic from haiku.go:73-118
- **PATTERN**: Environment variable handling from haiku.go:38-45 (OPENROUTER_API_KEY)
- **IMPORTS**:
  ```go
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
      "time"
      "github.com/steveyegge/beads/internal/types"
  )
  ```
- **GOTCHA**: Set Authorization header as "Bearer <key>", not just key
- **GOTCHA**: OpenRouter model format is "organization/model" (e.g., "anthropic/claude-3-5-haiku")
- **GOTCHA**: Set HTTP-Referer header (optional but recommended by OpenRouter)
- **VALIDATE**: `go test -short ./internal/compact -run TestOpenRouterProvider`

### CREATE internal/compact/provider_openrouter_test.go

- **IMPLEMENT**: TestNewOpenRouterProvider_RequiresAPIKey validates error when key missing
- **IMPLEMENT**: TestNewOpenRouterProvider_EnvVarHandling validates OPENROUTER_API_KEY usage
- **IMPLEMENT**: TestOpenRouterProvider_Summarize with httptest.Server mocking responses
- **IMPLEMENT**: TestOpenRouterProvider_Retry validates retry on 429 and 5xx
- **IMPLEMENT**: TestOpenRouterProvider_NoRetryOn4xx validates immediate failure on 400
- **PATTERN**: Test structure from haiku_test.go:13-50 for env var tests
- **IMPORTS**:
  ```go
  import (
      "context"
      "encoding/json"
      "errors"
      "net/http"
      "net/http/httptest"
      "strings"
      "testing"
      "github.com/steveyegge/beads/internal/types"
  )
  ```
- **VALIDATE**: `go test -short ./internal/compact -run TestOpenRouter`

### UPDATE go.mod

- **ADD**: `google.golang.org/genai` dependency for Gemini SDK
- **IMPORTS**: Run `go get google.golang.org/genai`
- **VALIDATE**: `go mod tidy && go mod verify`

### CREATE internal/compact/provider_gemini.go

- **IMPLEMENT**: GeminiProvider struct with genai.Client field
- **IMPLEMENT**: NewGeminiProvider constructor using genai.NewClient(ctx, nil)
- **IMPLEMENT**: Summarize method calling client.Models.GenerateContent(ctx, model, content)
- **IMPLEMENT**: Prompt construction matching tier1PromptTemplate format
- **IMPLEMENT**: Response parsing from Gemini format (Candidates[0].Content.Parts[0].Text)
- **IMPLEMENT**: Error handling with retry logic (Gemini SDK may have different error types)
- **PATTERN**: Constructor pattern from haiku.go:38-61 (API key validation)
- **PATTERN**: Environment variable handling for GEMINI_API_KEY
- **IMPORTS**:
  ```go
  import (
      "context"
      "errors"
      "fmt"
      "os"
      "github.com/steveyegge/beads/internal/types"
      "google.golang.org/genai"
  )
  ```
- **GOTCHA**: Gemini client uses context for initialization, must check ctx.Err() before operations
- **GOTCHA**: GEMINI_API_KEY environment variable must be set (SDK reads it automatically)
- **GOTCHA**: Model names for Gemini are simple strings (e.g., "gemini-2.5-flash")
- **VALIDATE**: `go test -short ./internal/compact -run TestGeminiProvider`

### CREATE internal/compact/provider_gemini_test.go

- **IMPLEMENT**: TestNewGeminiProvider_RequiresAPIKey validates error when GEMINI_API_KEY missing
- **IMPLEMENT**: TestNewGeminiProvider_EnvVarHandling validates API key is read from environment
- **IMPLEMENT**: TestGeminiProvider_Summarize validates prompt construction and response parsing
- **PATTERN**: Test structure from haiku_test.go:13-50
- **IMPORTS**:
  ```go
  import (
      "context"
      "errors"
      "testing"
      "github.com/steveyegge/beads/internal/types"
  )
  ```
- **GOTCHA**: May need to mock Gemini SDK calls (check SDK documentation for testing patterns)
- **VALIDATE**: `go test -short ./internal/compact -run TestGemini`

### UPDATE internal/config/config.go

- **ADD**: Default configuration for LLM provider settings after line 96
  ```go
  v.SetDefault("llm.provider", "anthropic")
  v.SetDefault("llm.anthropic.model", "claude-3-5-haiku-20241022")
  v.SetDefault("llm.openrouter.model", "anthropic/claude-3-5-haiku")
  v.SetDefault("llm.gemini.model", "gemini-2.5-flash")
  ```
- **ADD**: Explicit environment variable bindings after line 92
  ```go
  _ = v.BindEnv("llm.provider", "BEADS_LLM_PROVIDER")
  _ = v.BindEnv("llm.anthropic.api-key", "ANTHROPIC_API_KEY")
  _ = v.BindEnv("llm.openrouter.api-key", "OPENROUTER_API_KEY")
  _ = v.BindEnv("llm.gemini.api-key", "GEMINI_API_KEY")
  _ = v.BindEnv("llm.anthropic.model", "ANTHROPIC_MODEL")
  _ = v.BindEnv("llm.openrouter.model", "OPENROUTER_MODEL")
  _ = v.BindEnv("llm.gemini.model", "GEMINI_MODEL")
  ```
- **PATTERN**: Existing binding pattern from config.go:90-91
- **GOTCHA**: Keep ANTHROPIC_API_KEY binding for backward compatibility
- **VALIDATE**: `go test ./internal/config -run TestGetString`

### UPDATE internal/compact/compactor.go

- **REFACTOR**: Change `haikuClient` field to `provider Provider` interface
- **UPDATE**: New() constructor to accept provider type parameter
- **UPDATE**: CompactTier1() to call provider.Summarize() instead of haikuClient.SummarizeTier1()
- **MIRROR**: Keep all existing compactor logic (batching, concurrency) unchanged
- **PATTERN**: Interface usage allows swapping implementations without changing compactor logic
- **IMPORTS**: Add `"github.com/steveyegge/beads/internal/config"`
- **VALIDATE**: `go test -short ./internal/compact -run TestCompactor`

### UPDATE cmd/bd/compact.go

- **ADD**: `--provider` flag to compactCmd flags (line 1143)
  ```go
  compactCmd.Flags().StringVar(&compactProvider, "provider", "", "LLM provider (anthropic, openrouter, gemini)")
  ```
- **ADD**: Global variable for provider flag after line 35
  ```go
  compactProvider string
  ```
- **UPDATE**: runCompactAll and runCompactSingle functions (lines 190-220)
  - Read provider from flag or config (default to "anthropic")
  - Read appropriate API key based on provider type
  - Pass provider type to compact.New() constructor
- **UPDATE**: Error messages to mention provider selection when API key is missing
- **PATTERN**: Flag handling from existing compact flags (lines 1143-1166)
- **IMPORTS**: Add `"github.com/steveyegge/beads/internal/config"`
- **GOTCHA**: Provider flag should override config file setting if specified
- **VALIDATE**: `go build ./cmd/bd && ./bd compact --help | grep provider`

### UPDATE cmd/bd/compact.go runCompactRPC

- **UPDATE**: RPC call to include provider type in args map (line 558)
  ```go
  args := map[string]interface{}{
      "tier":       compactTier,
      "dry_run":    compactDryRun,
      "force":      compactForce,
      "all":        compactAll,
      "provider":   providerType,  // NEW
      "api_key":    apiKey,
      "workers":    compactWorkers,
      "batch_size": compactBatch,
  }
  ```
- **PATTERN**: Existing args map construction from compact.go:558-569
- **VALIDATE**: Manual test with daemon mode after full implementation

### DEPRECATED: Mark haiku.go for future removal

- **ADD**: Deprecation comment at top of haiku.go
  ```go
  // Package compact provides AI-powered issue compaction using Claude Haiku.
  //
  // DEPRECATED: This file is kept for backward compatibility.
  // New code should use provider_anthropic.go instead.
  ```
- **GOTCHA**: Don't remove haiku.go yet - keep for one release cycle for compatibility
- **VALIDATE**: No action needed - documentation only

---

## TESTING STRATEGY

### Unit Tests

**Scope**: Each provider implementation must have comprehensive unit tests covering:
- Constructor validation (API key required)
- Environment variable handling (precedence and fallback)
- Prompt rendering (UTF-8 handling, empty fields)
- Error handling (retryable vs non-retryable)
- Context cancellation

**Test Fixtures**:
```go
// Sample issue for testing
testIssue := &types.Issue{
    ID:                 "bd-test",
    Title:              "Fix authentication bug",
    Description:        "Users can't log in with OAuth",
    Design:             "Add error handling to OAuth flow",
    AcceptanceCriteria: "Users can log in successfully",
    Notes:              "Related to issue bd-2",
    Status:             types.StatusClosed,
}
```

**Mocking Strategy**:
- Anthropic: Use real SDK client (requires API key in CI, or skip tests)
- OpenRouter: Use `httptest.Server` to mock HTTP responses
- Gemini: Mock SDK client if possible, otherwise skip integration tests

### Integration Tests

**Scope**: End-to-end compaction workflow with each provider

**Test Cases**:
1. **Provider Selection via Environment Variable**
   - Set `BEADS_LLM_PROVIDER=openrouter`
   - Run `bd compact --auto --dry-run`
   - Verify OpenRouter provider is selected

2. **Provider Selection via Flag**
   - Run `bd compact --auto --provider gemini --dry-run`
   - Verify flag overrides environment variable

3. **Backward Compatibility**
   - Set only `ANTHROPIC_API_KEY` (no BEADS_LLM_PROVIDER)
   - Run `bd compact --auto --dry-run`
   - Verify Anthropic provider is used by default

4. **API Key Validation**
   - Run `bd compact --auto --provider openrouter` without `OPENROUTER_API_KEY`
   - Verify helpful error message mentioning which key is required

### Edge Cases

1. **Invalid Provider Name**
   - Run `bd compact --auto --provider invalid`
   - Expect error: "unknown provider: invalid (valid options: anthropic, openrouter, gemini)"

2. **Multiple API Keys Set**
   - Set ANTHROPIC_API_KEY, OPENROUTER_API_KEY, and GEMINI_API_KEY
   - Verify provider selection logic uses configured provider, not whichever key is present

3. **Context Cancellation During API Call**
   - Cancel context mid-request
   - Verify no retry attempts and immediate return with context.Canceled error

4. **Rate Limiting (429)**
   - Mock 429 response from OpenRouter
   - Verify retry with exponential backoff (1s, 2s, 4s)

5. **Network Timeout**
   - Mock slow response exceeding timeout
   - Verify retry for network errors

6. **Large Issue Content**
   - Create issue with 10KB description
   - Verify all providers handle large prompts (may hit token limits)

---

## VALIDATION COMMANDS

Execute every command to ensure zero regressions and 100% feature correctness.

### Level 1: Build & Type Check

```bash
# Go build (must succeed with no errors)
go build ./cmd/bd

# Go vet (must pass with 0 warnings)
go vet ./...

# Go mod verify (ensure dependencies are valid)
go mod verify
```

**Expected**: All commands pass with exit code 0

### Level 2: Unit Tests

```bash
# Run all compact package tests
go test -v ./internal/compact

# Run all config tests
go test -v ./internal/config

# Run short tests only (skip integration tests)
go test -short ./...

# Run with coverage
go test -cover ./internal/compact
```

**Expected**: All tests pass, coverage > 80% for new provider files

### Level 3: Integration Tests

```bash
# Test provider factory with all types
go test -v ./internal/compact -run TestNewProvider

# Test Anthropic provider (requires ANTHROPIC_API_KEY in environment or skip)
ANTHROPIC_API_KEY=sk-test-key go test -v ./internal/compact -run TestAnthropic

# Test OpenRouter provider with mocked HTTP
go test -v ./internal/compact -run TestOpenRouter

# Test Gemini provider (requires GEMINI_API_KEY or mock)
GEMINI_API_KEY=test-key go test -v ./internal/compact -run TestGemini
```

**Expected**: All tests pass or skip gracefully if API keys not available

### Level 4: Manual CLI Validation

```bash
# Test help text shows new --provider flag
./bd compact --help | grep -i provider

# Test default provider (Anthropic, backward compat)
./bd compact --auto --dry-run

# Test provider selection via flag
./bd compact --auto --provider openrouter --dry-run
./bd compact --auto --provider gemini --dry-run

# Test provider selection via environment variable
BEADS_LLM_PROVIDER=openrouter ./bd compact --auto --dry-run

# Test API key validation error messages
./bd compact --auto --provider openrouter  # Should error: OPENROUTER_API_KEY required

# Test invalid provider name
./bd compact --auto --provider invalid  # Should error: unknown provider
```

**Expected**: All commands produce expected output or errors

### Level 5: End-to-End Compaction Test

**Prerequisites**: Set up test database with eligible issues

```bash
# Initialize test database
export BEADS_DB=/tmp/beads-test.db
./bd init --quiet

# Create and close test issues
./bd create "Test issue 1" --description="Long description to compact" -t task -p 2
./bd close bd-<id> --reason "Testing compaction"

# Wait for issue to age (or modify closed_at timestamp in DB for testing)
# In production, Tier 1 requires 30+ days closed

# Test compaction with each provider
ANTHROPIC_API_KEY=sk-xxx ./bd compact --auto --id bd-<id> --force --provider anthropic
OPENROUTER_API_KEY=sk-xxx ./bd compact --auto --id bd-<id> --force --provider openrouter
GEMINI_API_KEY=xxx ./bd compact --auto --id bd-<id> --force --provider gemini
```

**Expected**: Issue is successfully compacted, summary is shorter than original

---

## ACCEPTANCE CRITERIA

- [ ] Provider interface is defined with Summarize method
- [ ] Factory function creates providers based on type string
- [ ] AnthropicProvider implements Provider interface (refactored from haiku.go)
- [ ] OpenRouterProvider implements Provider interface with HTTP client
- [ ] GeminiProvider implements Provider interface with official SDK
- [ ] Configuration supports llm.provider, llm.{provider}.api-key, llm.{provider}.model
- [ ] --provider flag allows runtime provider selection
- [ ] BEADS_LLM_PROVIDER environment variable selects provider
- [ ] Backward compatibility: ANTHROPIC_API_KEY still works without BEADS_LLM_PROVIDER
- [ ] All providers support retry logic with exponential backoff
- [ ] All providers distinguish retryable vs non-retryable errors
- [ ] Unit tests pass for all providers (>80% coverage)
- [ ] Integration tests verify end-to-end compaction with each provider
- [ ] Error messages mention specific API key required for selected provider
- [ ] Documentation updated (README.md, docs/CLI_REFERENCE.md)
- [ ] go mod tidy succeeds with new dependencies
- [ ] Build succeeds with no warnings or errors

---

## COMPLETION CHECKLIST

- [ ] All tasks completed in order
- [ ] Each task validation passed immediately
- [ ] All validation commands executed successfully:
  - [ ] Level 1: go build, go vet, go mod verify
  - [ ] Level 2: Unit tests with coverage >80%
  - [ ] Level 3: Integration tests (all providers)
  - [ ] Level 4: CLI manual validation
  - [ ] Level 5: End-to-end compaction test
- [ ] Full test suite passes (unit + integration)
- [ ] No build warnings or errors
- [ ] go mod verify succeeds
- [ ] All acceptance criteria met
- [ ] Code reviewed for quality and maintainability
- [ ] Backward compatibility verified (ANTHROPIC_API_KEY still works)

---

## NOTES

### Design Decisions

**Why provider abstraction instead of direct implementations?**
- Enables future providers (OpenAI, Cohere, local models) without changing compactor
- Testable: can mock Provider interface for compactor tests
- Separation of concerns: compaction logic vs LLM API details

**Why keep haiku.go instead of deleting immediately?**
- Deprecation period allows users to migrate gradually
- Reduces risk of breaking existing automation
- Can be removed in next major version (v1.0 or v2.0)

**Why OpenRouter uses HTTP client instead of SDK?**
- No official Go SDK exists for OpenRouter
- OpenAI-compatible API is simple enough for direct HTTP
- Reduces dependencies compared to unofficial third-party SDKs

**Why Gemini uses official SDK instead of HTTP?**
- Google provides official `google.golang.org/genai` package
- SDK handles authentication, retry, and error types
- Better long-term maintainability with official support

### Trade-offs

**Configuration Complexity**: Added 7 new config keys (llm.provider, llm.{provider}.api-key, llm.{provider}.model). Alternative was simpler but less flexible (single BEADS_API_KEY). Chose flexibility to support provider-specific models.

**Backward Compatibility**: Kept ANTHROPIC_API_KEY instead of requiring BEADS_LLM_ANTHROPIC_API_KEY. Trade-off: slightly inconsistent naming, but prevents breaking existing workflows.

**Testing Strategy**: OpenRouter uses mocked HTTP (fast, reliable), Anthropic/Gemini tests may require real API keys (slower, may hit rate limits). Alternative was mock all providers, but real API tests catch SDK changes.

### Performance Implications

- OpenRouter HTTP client: ~100-200ms latency per request (comparable to Anthropic SDK)
- Gemini SDK: ~80-150ms latency per request (Google's infrastructure is fast)
- No significant performance regression expected vs current Anthropic-only implementation

### Security Considerations

- API keys stored in environment variables (never logged or committed)
- HTTP client for OpenRouter uses TLS (https://openrouter.ai)
- Gemini SDK handles authentication securely
- No plaintext API key storage in config files (only environment variables)

### Future Extensions

**Potential provider additions**:
- OpenAI (direct API, not via OpenRouter)
- Cohere
- Local models via Ollama
- Hugging Face Inference API

**Potential features**:
- Provider fallback chains (try Anthropic, fallback to OpenRouter if rate limited)
- Cost tracking per provider
- Model selection via config (different models for Tier 1 vs Tier 2)
- Custom prompt templates per provider

---

## Sources

- [OpenRouter API Documentation](https://openrouter.ai/docs/api/reference/overview)
- [OpenRouter API Parameters](https://openrouter.ai/docs/api/reference/parameters)
- [OpenRouter Authentication](https://openrouter.ai/docs/api/reference/authentication)
- [Gemini API Documentation](https://ai.google.dev/gemini-api/docs)
- [Gemini API Quickstart](https://ai.google.dev/gemini-api/docs/quickstart)
- [Gemini API Reference](https://ai.google.dev/api)
- [Gemini API Libraries](https://ai.google.dev/gemini-api/docs/libraries)
