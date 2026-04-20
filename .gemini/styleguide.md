# OpenPerouter Code Review Guidelines

## General Contributing Guidelines

Please refer to our comprehensive contributing guide at [website/content/docs/contributing/_index.md](../website/content/docs/contributing/_index.md) for:

- Code organization and project structure
- Building and running the code
- Running tests locally
- Commit message guidelines
- How to extend the end-to-end test suite

Also see [Claude.md](../Claude.md) for detailed Go style guidelines including:

- Code readability and "line of sight" principles
- Line length guidelines (120 character limit)
- Package organization best practices
- Error handling patterns
- Dependency management
- Control flow recommendations

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
