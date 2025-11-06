@@ hello
# Project: OpenPERouter

## Overview
OpenPERouter is a Kubernetes-native router component that runs on cluster nodes and exposes VPN entry points. For detailed architecture and design information, see website/content.

## Tech Stack
- **Language:** Go
- **BGP Implementation:** FRR (Free Range Routing) - https://frrouting.org/
- **Platform:** Kubernetes

## Development Workflow

### Project Structure
Project layout, build commands, and test instructions are documented in @website/content/docs/contributing/_index.md

### Testing Strategy

**Unit Tests:**
Run `make test` to execute unit tests.

**End-to-End Tests:**
- Command: `make e2etests`
- **Prerequisites:** Requires active deployment
- **Before running:** Always verify cluster is running and deployment version is current
- **Validation only:** You can run the command to check if e2e test code compiles, but confirm with the user before executing against a cluster

**Development environment**

The development environment uses kind and containerlab. Details can be found in @website/content/docs/contributing/devenv.md

### Common Build Commands
- `make build` - Build binaries
- `make docker-build` - Build images
- `make help` - Show available make targets

## Go Style Guidelines

You can run `make lint` to run the go linter against the codebase.

### Code Readability: Line of Sight

Write code that's easy to scan vertically:

1. **Happy Path Left-Aligned:** Keep the main execution path with minimal indentation
2. **Early Returns:** Exit as soon as conditions are met to reduce nesting
3. **Avoid else-returns:** Invert conditions to return early instead of using else blocks
4. **Extract Functions:** Break large functions into smaller, single-purpose functions

**Example:**
```go
// Good
func Process(data string) error {
    if data == "" {
        return errors.New("empty data")
    }
    if !isValid(data) {
        return errors.New("invalid data")
    }
    // happy path continues here
    return doWork(data)
}

// Avoid
func Process(data string) error {
    if data != "" {
        if isValid(data) {
            return doWork(data)
        } else {
            return errors.New("invalid data")
        }
    } else {
        return errors.New("empty data")
    }
}
```

### Package Organization

**Naming:**
- Use descriptive, single-word names that convey purpose
- AVOID generic names: `util`, `common`, `lib`, `misc`, `helpers`
- Package name should be an "elevator pitch" for its functionality

**File Structure:**
- Name the primary file after the package (e.g., `network.go` in package `network`)
- Place public APIs and important types at the top of files
- Place internal helpers and implementation details at the bottom
- Utility functions should be in separate files within the package

### Error Handling

1. **Type-Safe Checking:** Use `errors.Is` and `errors.As` instead of string comparison
2. **Add Context:** Wrap errors with `fmt.Errorf` and `%w` to preserve the error chain
3. **Propagate Context:** Each layer should add meaningful context about what operation failed

**Example:**
```go
if err := fetchData(id); err != nil {
    return fmt.Errorf("failed to fetch data for id %s: %w", id, err)
}
```

### Dependency Management

**Environment Variables:**
- NEVER read environment variables from packages
- ALWAYS read them in `main()` function
- Pass values explicitly through function parameters or configuration structs

**Function Arguments:**
- Use pointer arguments when the function needs to modify the argument
- Use value arguments for read-only parameters

### Control Flow

**Switch vs If-Else:**
- Prefer `switch` statements for multiple conditions
- Go's switch supports embedded conditions - use this feature

**Named Returns:**
- Avoid named return values - they can obscure control flow
- Use explicit return statements for clarity

**Goroutines in Controllers:**
- Exercise caution when spawning goroutines in Kubernetes controllers
- Controller lifecycle management makes goroutine cleanup complex
- Prefer controller-runtime's built-in concurrency patterns

## Code Review Checklist

When reviewing or writing code, verify:
- [ ] Happy path is left-aligned with early returns
- [ ] No generic package names (util, common, etc.)
- [ ] Errors wrapped with context using `%w`
- [ ] No environment variable reads outside main()
- [ ] Package-named entry point file exists
- [ ] Public APIs at top of file, helpers at bottom
- [ ] Switch used instead of long if-else chains
- [ ] No named returns unless absolutely necessary
- [ ] Goroutines in controllers are carefully managed
