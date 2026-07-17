# OpenPerouter Code Review Guidelines

## General Contributing Guidelines

Please refer to our comprehensive contributing guide at [website/content/docs/contributing/_index.md](../website/content/docs/contributing/_index.md) for:

- Code organization and project structure
- Building and running the code
- Running tests locally
- Commit message guidelines
- How to extend the end-to-end test suite

### Use Modern Go Features

Always check the Go version in `go.mod` and prefer language features and standard library additions from that version
over third-party helpers or older patterns.

**Example - Go 1.26 `new()` builtin:**
```go
// Good: use the builtin
val := new("hello")

// Avoid: third-party pointer helpers
val := ptr.To("hello")
```

When writing new code, modifying existing code, or reviewing code, prefer the latest idiomatic constructs available in
the project's Go version rather than relying on utility packages that duplicate standard functionality.

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

### Code Readability: Line Length

Limit line length to 120 characters whenever possible:
- Break long function calls, struct definitions, and statements into multiple lines
- Use appropriate indentation for continuation lines
- Prioritize readability over strict adherence when necessary

### Package Organization

**Naming:**
- Use descriptive, single-word names that convey purpose
- AVOID generic names: `util`, `common`, `lib`, `misc`, `helpers`
- Package name should be an "elevator pitch" for its functionality

**File Structure:**
- Name the primary file after the package (e.g., `network.go` in package `network`)
- Place public APIs and important types at the top of files
- **Place helper functions at the bottom of files, after where they are used**
  - This applies to ALL files (production code and tests)
  - Main/exported functions first, internal helpers last
  - In test files: test functions first, helper functions at the bottom
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



### Unnecessary Comments
Remove obvious comments that restate what the code already says. Comments should explain *why*, never *what*. AI-generated boilerplate comments are especially problematic — they add noise without value.

**Bad:** `// Create the client` above `client := NewClient()`
**Good:** `// Retry with a new client because FRR drops idle connections after 90s`

If removing the comment wouldn't confuse a future reader, remove it.

### Code Organization — Newspaper Structure
Follow [newspaper code structure](https://kentcdodds.com/blog/newspaper-code-structure): public/exported functions at the top, private/utility functions at the bottom. Callers before callees. This makes files scannable — readers find the "what" first, the "how" below.

- Exported methods and types go at the top of the file
- Exception: `init()` functions go at the top, near package-level vars they initialize
- Unexported helpers go at the bottom
- When a function calls another function in the same file, the caller should appear above the callee

### Flat Control Flow — Early Returns Over Nested Ifs
Avoid complex nested `if` conditions. Especially if they include ors and mix ands and ors or negations.

**Bad:** complex condition guarding a nested block inline
```go
if (gvk.Group == "" && gvk.Kind == "Service") || gvk.Kind == "Foo" {
    // logic
}
```

**Good:** extract the block into a function with early returns for each condition
```go

func isKindToSkip(gvk *gvk) bool {
    if gvk.Kind == "Foo" {
        return true
    }
    if gvk.Group == "" && gvk.Kind == "Service" {
        return true
    }
    return false
}
```

```go
if isKindToSkip(gvk) {
    // logic
}
```

### Return Early — Avoid Intermediate Variables
When each branch of a switch or if/else produces a complete result, return it directly instead of assigning to intermediate variables and returning at the end. This makes each branch self-contained and avoids tracking state across the function.

**Bad:** accumulate into variables, return once at the end
```go
func bridgeName(spec BridgeSpec) string {
    var name string
    switch spec.Type {
    case Linux:
        name = spec.LinuxBridge.Name
    case OVS:
        name = spec.OVSBridge.Name
    }
    return name
}
```

**Good:** return directly from each branch
```go
func bridgeName(spec BridgeSpec) string {
    switch spec.Type {
    case Linux:
        return spec.LinuxBridge.Name
    case OVS:
        return spec.OVSBridge.Name
    }
    return ""
}
```

### Simplification — Remove Unnecessary Wrappers
Push back on overengineered solutions. If a wrapper, abstraction, or indirection doesn't earn its complexity, remove it.

Watch for:
- Wrapper functions that just forward to another function — let the consumer call the inner function directly
- Methods that should be plain functions (no receiver state used)
- Generic solutions for problems that only have one or two concrete cases
- Premature abstractions ("in case we need to support X later")

The test: if removing the abstraction makes the code shorter and equally clear, remove it.

## Code Review Focus Areas

When reviewing code, pay special attention to:

### 1. Correctness
- Does the code implement the intended functionality correctly?
- Are edge cases handled properly?
- Is error handling comprehensive and appropriate?

### 2. Security
- Are all external inputs validated?
- Are credentials and secrets handled securely?
- Are there any potential security vulnerabilities (injection, DoS, etc.)?
- Is network isolation properly maintained between VPN tunnels?

### 3. Testing
- Are there adequate unit tests for new functionality?
- Are error paths tested?
- For new features, are e2e tests added to `/e2etest`?
- Do tests follow table-driven test patterns where appropriate?

### 4. Code Quality
- Is the code clear and maintainable?
- Are functions focused and reasonably sized?
- Are variable and function names descriptive?
- Is complex logic documented with comments explaining "why"?

### 5. Go Best Practices
- Does the code follow standard Go conventions?
- Is the happy path left-aligned with early returns? (see Claude.md)
- Are errors wrapped with context using `%w`?
- Are errors handled explicitly (not ignored)?
- Is proper resource cleanup done (using defer)?
- For concurrent code, are goroutines managed properly?
- Are package names descriptive (avoiding generic names like `util`, `common`)?
- Are environment variables only read in `main()`?

### 6. Kubernetes Operator Patterns
- Is reconciliation logic idempotent?
- Are status conditions used appropriately?
- Do CRD changes include proper validation schemas?
- Are API conventions followed?

### 7. Configuration Changes
- If CRDs are modified, has `make bundle` been run?
- Are all-in-one manifests updated via `make generate-all-in-one`?
- Are Helm charts updated if needed?

### 8. Performance
- Are there any obvious performance concerns in hot paths?
- Is memory usage reasonable, especially in network data processing?
- Are unnecessary allocations avoided?

### 9. Documentation
- Are complex changes documented?
- Is the website documentation updated if needed?
- Are godoc comments present for exported functions and types?

## OpenPerouter-Specific Considerations

### BGP and Routing
- Validate BGP configurations thoroughly
- Ensure route announcements are correct
- Handle BGP session lifecycle properly

### VPN Tunneling
- Validate VPN configurations
- Ensure proper encapsulation/decapsulation
- Test multi-tenant isolation

### FRR Integration
- Verify FRR configuration generation is correct
- Test FRR configuration updates
- Ensure host network changes are safe

## References

- [Effective Go](https://golang.org/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Kubernetes API Conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)

## Code Review Checklist

When reviewing or writing code, verify:
- [ ] Happy path is left-aligned with early returns
- [ ] No generic package names (util, common, etc.)
- [ ] Errors wrapped with context using `%w`
- [ ] No environment variable reads outside main()
- [ ] Package-named entry point file exists
- [ ] Helper functions placed at bottom of file (after where they are used)
- [ ] Switch used instead of long if-else chains
- [ ] No named returns unless absolutely necessary
- [ ] Goroutines in controllers are carefully managed
