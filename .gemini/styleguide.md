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
- Package organization best practices
- Error handling patterns
- Dependency management
- Control flow recommendations

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
