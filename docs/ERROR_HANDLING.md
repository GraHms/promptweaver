# Promptweaver Error Handling

This document describes the error handling capabilities in Promptweaver.

## Error Types

Promptweaver provides several specialized error types to help diagnose parsing issues:

### ParseError

The base error type for all parsing errors. Contains:
- Position information (line/column)
- Error message
- Context showing surrounding content

### MalformedTagError

Indicates a problem with tag syntax, such as:
- Missing closing quotes in attributes
- Invalid tag name
- Unclosed tags

Example:
```go
if err, ok := err.(*MalformedTagError); ok {
    fmt.Printf("Malformed tag %s: %s\n", err.TagName, err.Message)
}
```

### AttributeParsingError

Indicates a problem with attribute parsing, such as:
- Missing equals sign
- Invalid attribute value format
- Duplicate attributes

Example:
```go
if err, ok := err.(*AttributeParsingError); ok {
    fmt.Printf("Error in attribute %s of tag %s: %s\n", 
        err.AttributeName, err.TagName, err.Message)
}
```

### UnmatchedTagError

Indicates a closing tag with no matching opening tag.

Example:
```go
if err, ok := err.(*UnmatchedTagError); ok {
    fmt.Printf("Unmatched closing tag %s\n", err.TagName)
}
```

### ValidationError

Indicates that section content failed validation.

Example:
```go
if err, ok := err.(*ValidationError); ok {
    fmt.Printf("Validation failed for section %s: %s\n", 
        err.SectionName, err.Message)
}
```

## Recovery Modes

Promptweaver supports two recovery modes:

### StrictMode (Default)

In strict mode, parsing stops on the first error. This is the default behavior.

```go
// Default is StrictMode
engine := NewEngine(registry)
```

### ContinueMode

In continue mode, the parser attempts to recover from errors and continue parsing.

```go
// Use continue mode
engine := NewEngineWithOptions(registry, WithContinueMode())
```

## Custom Error Handling

You can provide a custom error handler function to control how errors are handled:

```go
// Create a custom error handler
errorHandler := func(err error) bool {
    // Log the error
    log.Printf("Parsing error: %v", err)
    
    // Return true to continue parsing, false to stop
    return true
}

// Use the custom error handler
engine := NewEngineWithOptions(registry, WithErrorHandler(errorHandler))
```

The error handler receives the error and returns a boolean indicating whether to continue parsing.

## Content Validation

Promptweaver allows you to validate section content using validators:

### Regex Validation

```go
// Register a regex validator for code sections
engine.RegisterRegexValidator("code", "^func\\s+\\w+", 
    "must start with a function declaration")
```

### Custom Validation Functions

```go
// Register a custom validator for JSON sections
engine.RegisterFuncValidator("json", func(sectionName, content string, pos Position) error {
    var js map[string]interface{}
    if err := json.Unmarshal([]byte(content), &js); err != nil {
        return NewValidationError(pos, sectionName, 
            "invalid JSON: " + err.Error(), content)
    }
    return nil
})
```

## Position Information

All errors include position information (line/column) to help locate the issue in the input:

```go
if parseErr, ok := err.(*ParseError); ok {
    fmt.Printf("Error at %s: %s\n", parseErr.Pos, parseErr.Message)
}
```

## Context Information

Errors include context showing the surrounding content, which helps with debugging:

```
malformed tag <think> at line 5, column 10: expected '>' after attribute name
Context: 
   3: <summary>Some content</summary>
   4: 
-> 5: <think attr missing-equals>
          ^
   6:   Content
   7: </think>
```

## Example: Handling Different Error Types

```go
func processWithErrorHandling(input string) error {
    // Setup engine and sink
    reg := NewRegistry()
    reg.Register(SectionPlugin{Name: "think"})
    sink := NewHandlerSink()
    
    // Use continue mode
    engine := NewEngineWithOptions(reg, WithContinueMode())
    
    // Process the input
    err := engine.ProcessStream(ReaderFromString(input), sink)
    if err != nil {
        switch e := err.(type) {
        case *MalformedTagError:
            fmt.Printf("Malformed tag %s at %s: %s\n", 
                e.TagName, e.Pos, e.Message)
        case *AttributeParsingError:
            fmt.Printf("Attribute error in tag %s at %s: %s\n", 
                e.TagName, e.Pos, e.Message)
        case *ValidationError:
            fmt.Printf("Validation failed for section %s: %s\n", 
                e.SectionName, e.Message)
        default:
            fmt.Printf("Error: %v\n", err)
        }
        return err
    }
    return nil
}
```

## Best Practices

1. **Use appropriate recovery mode**: Use StrictMode during development to catch all issues, and ContinueMode in production to maximize resilience.

2. **Provide custom error handlers**: Custom handlers allow you to log errors while continuing to process valid sections.

3. **Add content validators**: Validators ensure that section content meets your requirements before it's processed by handlers.

4. **Check error types**: Use type assertions to handle different error types appropriately.

5. **Include context in error messages**: When reporting errors to users, include the position and context to help them locate and fix the issue.