# Next Steps: OpenRouter and Gemini LLM Provider Support

## Implementation Status: ✅ COMPLETE

All code has been implemented for adding OpenRouter and Gemini provider support to the `bd compact --auto` command.

## Validation Commands

Run these commands to validate the implementation:

```bash
# 1. Update go.sum with new dependencies
go mod tidy

# 2. Verify build succeeds
go build ./cmd/bd

# 3. Run all compact package tests
go test -v ./internal/compact/...

# 4. Check for linting issues
go vet ./internal/compact/...

# 5. Run full test suite (optional)
go test ./...
```

## Expected Results

- `go mod tidy`: Should add entries for Gemini SDK dependencies to go.sum
- `go build`: Should compile without errors
- `go test`: All tests should pass (some may be skipped if API keys not set)
- `go vet`: Should report no issues

## Testing the Feature

### Test with Anthropic (default)
```bash
export ANTHROPIC_API_KEY=sk-ant-...
bd compact --auto --dry-run
```

### Test with OpenRouter
```bash
export OPENROUTER_API_KEY=sk-or-...
bd compact --auto --provider openrouter --dry-run
```

### Test with Gemini
```bash
export GEMINI_API_KEY=...
bd compact --auto --provider gemini --dry-run
```

### Test provider selection via config
```bash
# Add to .beads/config.yaml:
echo "llm.provider: openrouter" >> .beads/config.yaml

# Run without --provider flag (should use config)
bd compact --auto --dry-run
```

## Files Changed

### New Files (8)
- internal/compact/provider.go
- internal/compact/provider_test.go
- internal/compact/provider_anthropic.go
- internal/compact/provider_anthropic_test.go
- internal/compact/provider_openrouter.go
- internal/compact/provider_openrouter_test.go
- internal/compact/provider_gemini.go
- internal/compact/provider_gemini_test.go

### Modified Files (5)
- cmd/bd/compact.go - Added --provider flag and selection logic
- go.mod - Added Gemini SDK dependencies
- internal/compact/compactor.go - Uses Provider interface
- internal/compact/haiku.go - Marked DEPRECATED
- internal/config/config.go - Added LLM provider config

## What Was Implemented

1. **Provider Abstraction Layer**
   - Clean interface-based design
   - Factory pattern for provider creation
   - Support for three providers: Anthropic, OpenRouter, Gemini

2. **Configuration System**
   - Environment variables: BEADS_LLM_PROVIDER, ANTHROPIC_API_KEY, OPENROUTER_API_KEY, GEMINI_API_KEY
   - Config file support: llm.provider, llm.{provider}.api-key, llm.{provider}.model
   - Backward compatibility with existing ANTHROPIC_API_KEY

3. **CLI Integration**
   - `--provider` flag for runtime provider selection
   - Provider precedence: CLI flag > config file > default (anthropic)

4. **All Providers Include**
   - Retry logic with exponential backoff
   - Context cancellation support
   - Proper error handling (retryable vs non-retryable)
   - Shared prompt template for consistency

## Ready for Commit

Once validation passes, the implementation is ready to be committed!
