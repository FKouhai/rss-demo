# AGENTS.md

## Build/Lint/Test Commands

### Go Services (rss_notify, rss_poller, rss_config)
- Build: `nix build` (in service directory)
- Test: `nix flake check` or `go test ./...`
- Single test: `go test -run TestName ./path/to/package`
- Lint: `golangci-lint run` (requires golangci-lint)

### Frontend (rss_frontend)
- Dev server: `npm run dev`
- Build: `npm run build`
- Test: `npm test` (if available)

## Code Style Guidelines

### Go
- Use `go fmt` for formatting
- Use descriptive names (camelCase for variables/functions, PascalCase for exported)
- Handle errors explicitly
- Use context for cancellation/timeout
- Follow standard Go project layout
- Use zap for logging, otel for tracing

### Imports
- Group standard library, third-party, and local imports
- Use descriptive import aliases when needed

### Frontend (Astro/TypeScript)
- Use Prettier for formatting
- Follow Astro's style guide
- Use TypeScript for type safety