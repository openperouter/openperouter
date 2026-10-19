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
4. **Inline Error Checks:** When only the error is needed, combine the call and check with
   `if err := operation(); err != nil`. Keep a separate assignment when a returned value or the error is needed later.
5. **Handle Errors Immediately:** Check the error directly after the call, before using its results.
6. **Flatten Error Cases:** Check `errors.Is(err, sentinel)` directly before the general `err != nil` case
   instead of nesting it inside that case. It returns false for a nil error and a non-nil sentinel.
7. **Distinguish Failure Types:** Use sentinel errors or specific error types when callers need different recovery
   behavior. Do not treat every error as temporary or infer failure types from error strings.

**Example:**
```go
if err := fetchData(id); err != nil {
    return fmt.Errorf("failed to fetch data for id %s: %w", id, err)
}
```

**Avoid:** separating an error-only call from its check.

```go
err := store.Save(state)
if err != nil {
    return err
}
```

**Prefer:** scope the error to its check.

```go
if err := store.Save(state); err != nil {
    return fmt.Errorf("saving state for %s: %w", state.InterfaceName, err)
}
```

When a call also returns a value needed afterwards, keep the assignment separate and handle specific errors first:

```go
state, err := store.Load(interfaceName)
if errors.Is(err, devicestate.ErrNotFound) {
    return captureDeviceState(interfaceName)
}
if err != nil {
    return devicestate.State{}, fmt.Errorf("loading state for %s: %w", interfaceName, err)
}
return state, nil
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
- For independent exit conditions, prefer sequential `if` statements with early returns. Keep the remaining work
  together instead of spreading a single execution path across multiple switches and nested conditions.
- Initialize defaults first, then override them conditionally when this removes an unnecessary `else`.

```go
vrf := defaultVRFName
if params.VRF != "" {
    vrf = params.VRF
}
```

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

Explain current behavior and non-obvious assumptions, including ordering constraints and surprising backend behavior.
Do not describe the previous bug or announce that the code fixes it. Place the explanation where the behavior is owned
and avoid repeating it at both the caller and the helper.

```go
// Avoid: Fix the duplicate IPv6 address bug from issue #123.

// Prefer: The shadow interface owns the link-local address, so the backing
// interface must not generate another one.
```

### Code Organization — Newspaper Structure
Follow [newspaper code structure](https://kentcdodds.com/blog/newspaper-code-structure): public/exported functions at the top, private/utility functions at the bottom. Callers before callees. This makes files scannable — readers find the "what" first, the "how" below.

- Exported methods and types go at the top of the file
- Exception: `init()` functions go at the top, near package-level vars they initialize
- Unexported helpers go at the bottom
- When a function calls another function in the same file, the caller should appear above the callee

```go
func SaveState(path string, state State) error {
    data, err := encodeState(state)
    if err != nil {
        return err
    }
    return os.WriteFile(path, data, 0600)
}

func encodeState(state State) ([]byte, error) {
    return json.Marshal(state)
}
```

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

- Remove no-op functions and their invocations, unused helpers, and redundant branches.
- Reuse existing validation and setup logic instead of implementing parallel versions that can drift.
- Extract helpers when they name a meaningful operation or clarify an obscure expression.
- Keep single-scope functions: each function should perform one operation, such as fetching state or saving it.
- Keep templates focused on rendering. Compute configuration decisions in Go before passing data to the template.

**Keep single-scope functions.** Split `fetchAndSaveState` into fetch and save operations so callers can use each
independently.

**Avoid:** fetching and saving in the same function.

```go
func fetchAndSaveState(interfaceName string, store StateStore) error {
    state, err := fetchState(interfaceName)
    if err != nil {
        return err
    }
    return store.Save(state)
}
```

**Prefer:** let the caller compose the fetch and save operations.

```go
state, err := fetchState(interfaceName)
if err != nil {
    return err
}
if err := store.Save(state); err != nil {
    return fmt.Errorf("saving state for %s: %w", interfaceName, err)
}
```

Give obscure checks a name instead of making every caller interpret them:

```go
func isExecutable(mode os.FileMode) bool {
    return mode&0111 != 0
}
```

### Naming and Data Structures

- Name values for their meaning: distinguish desired configuration from observed state, such as `desiredIP`.
- Name functions for what they do. `Ensure` implies making a state true; use `Check`, `Validate`, or `Is...` for checks.
- Make conversion direction clear in names, such as `PCIAddressForNetlinkName`.
- Use named constants for meaningful thresholds and repeated configuration values instead of unexplained literals.
- Use named fields in struct literals when positional values, especially booleans, obscure their meaning.
- Represent sets with `map[T]struct{}` rather than `map[T]bool` when only membership matters.
- Embed structs only when their promoted fields or methods are part of the containing type's intended API.
  Otherwise use an explicit field or pass the required properties directly.
- Keep related calculations together, such as an address and its mask.

**Avoid:** unexplained values and positional fields.

```go
opts := portOptions{true, 4}
```

**Prefer:** named fields and constants that explain their purpose.

```go
const defaultRXQueues = 4

opts := portOptions{
    IsTap:    true,
    RXQueues: defaultRXQueues,
}
```

Use membership directly for a set, with the conventional `ok` name for the lookup result:

```go
configuredPorts := map[string]struct{}{
    "underlay0": {},
}
if _, ok := configuredPorts[portName]; ok {
    return nil
}
```

Use a named field when the contained type should not expose its fields through the containing type:

```go
type interfaceDetails struct {
    Properties interfaceProperties
    Driver     string
}
```

### Writing tests
On unit tests use only the go standard library / testify (at most).

For e2e tests, use the ginkgo / gomega framework.

- Extend existing table-driven tests where applicable. Use new tables for related cases instead of adding one-off
  test functions that diverge from the surrounding style.
- Put varying inputs, expected results, and expected errors in the test table. Pass explicit case parameters instead
  of deriving several behaviors from a boolean flag.
- Assert the specific result: compare the expected nodes or values, not merely that a result is non-empty.
- Match expected error types or messages instead of only checking that an error occurred. Prefer a complete message
  when it is stable and relevant to the behavior being tested.
- Compare complete expected structs instead of checking each field separately when the whole result is relevant.
- Give assertion failures enough context to identify the expected and actual results.
- Keep test literals readable: put expected values before assertions and break nested composite literals across lines.
- Reuse tests that already cover the behavior; avoid redundant cases or cases based on unsupported inputs.
- Keep active waits in e2e tests using Gomega's `Eventually` / `Consistently`. Shared helpers should perform a single
  check so tests control polling and timeouts.

**Prefer:** explicit inputs, complete expected structs, and expected errors in a table.
This example assumes `parsePortOptions` rejects an empty name with `errMissingPortName`.

```go
func TestParsePortOptions(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    portOptions
        wantErr error
    }{
        {
            name:  "PCI device",
            input: "0000:03:00.0",
            want: portOptions{
                PCIAddress: "0000:03:00.0",
            },
        },
        {
            name:    "missing name",
            wantErr: errMissingPortName,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := parsePortOptions(tt.input)
            require.ErrorIs(t, err, tt.wantErr)
            require.Equal(t, tt.want, got)
        })
    }
}
```

Keep polling in the e2e test and assert the desired result:

```go
Eventually(func() ([]string, error) {
    return selectedNodeNames(ctx, selector)
}).Should(ConsistOf("worker-0", "worker-1"))
```

### Writing gomega based tests
Favor using "positive" logic (i.e. expect success) over expecting an error
not to happen. Assert the outcome you want, not the absence of the outcome
you don't.

This reads better and fails better: `Succeed()` states the intent directly,
and when the call does fail Gomega prints the actual error in the failure
message.

**Bad:** expect the error not to happen
```go
err := doStuff()
Expect(err).NotTo(HaveOccurred())
```

**Good:** expect success
```go
Expect(doStuff()).To(Succeed())
```

The same idea applies to functions that return a value alongside the error.
Assert on the value directly and let Gomega check the error for you.

**Bad:** check the error, then the value
```go
got, err := lookup(id)
Expect(err).NotTo(HaveOccurred())
Expect(got).To(Equal(want))
```

**Good:** assert the value, error handled implicitly
```go
Expect(lookup(id)).To(Equal(want))
```

This applies to setup steps too: `Expect(k8sClient.Create(ctx, obj)).To(Succeed())`
keeps the precondition and its assertion on one line.

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
- [ ] Error-only calls use inline `if err := ...; err != nil` checks
- [ ] No environment variable reads outside main()
- [ ] Package-named entry point file exists
- [ ] Helper functions placed at bottom of file (after where they are used)
- [ ] Switch used for related alternatives; sequential early returns used for independent exit conditions
- [ ] Names describe intent; meaningful literals use named constants
- [ ] Helpers have focused responsibilities and reuse existing logic
- [ ] Comments explain current reasoning without bug history or repeated explanations
- [ ] Related test cases use tables with explicit inputs, expected results, and expected errors
- [ ] No named returns unless absolutely necessary
- [ ] Goroutines in controllers are carefully managed
