package promptweaver

import (
	"fmt"
	"strings"
)

// Position represents a position in the input stream.
type Position struct {
	Line   int // 1-based line number
	Column int // 1-based column number
}

// String returns a string representation of the position.
func (p Position) String() string {
	return fmt.Sprintf("line %d, column %d", p.Line, p.Column)
}

// ParseError is the base error type for all parsing errors.
type ParseError struct {
	Pos     Position // Position where the error occurred
	Message string   // Error message
	Context string   // Surrounding content for context
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	if e.Context != "" {
		return fmt.Sprintf("%s at %s\nContext: %s", e.Message, e.Pos, e.Context)
	}
	return fmt.Sprintf("%s at %s", e.Message, e.Pos)
}

// MalformedTagError represents an error when a tag is malformed.
type MalformedTagError struct {
	ParseError
	TagName string // Name of the malformed tag
}

// Error implements the error interface.
func (e *MalformedTagError) Error() string {
	return fmt.Sprintf("malformed tag <%s> at %s: %s\nContext: %s",
		e.TagName, e.Pos, e.Message, e.Context)
}

// AttributeParsingError represents an error when parsing tag attributes.
type AttributeParsingError struct {
	ParseError
	TagName       string // Name of the tag with the attribute error
	AttributeName string // Name of the problematic attribute, if known
}

// Error implements the error interface.
func (e *AttributeParsingError) Error() string {
	if e.AttributeName != "" {
		return fmt.Sprintf("error parsing attribute '%s' in tag <%s> at %s: %s\nContext: %s",
			e.AttributeName, e.TagName, e.Pos, e.Message, e.Context)
	}
	return fmt.Sprintf("error parsing attributes in tag <%s> at %s: %s\nContext: %s",
		e.TagName, e.Pos, e.Message, e.Context)
}

// UnmatchedTagError represents an error when a closing tag doesn't match any opening tag.
type UnmatchedTagError struct {
	ParseError
	TagName string // Name of the unmatched tag
}

// Error implements the error interface.
func (e *UnmatchedTagError) Error() string {
	return fmt.Sprintf("unmatched closing tag </%s> at %s\nContext: %s",
		e.TagName, e.Pos, e.Context)
}

// ValidationError represents an error when section content fails validation.
type ValidationError struct {
	ParseError
	SectionName string // Name of the section that failed validation
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed for section <%s> at %s: %s\nContext: %s",
		e.SectionName, e.Pos, e.Message, e.Context)
}

// NewParseError creates a new ParseError with context.
func NewParseError(pos Position, message, context string) *ParseError {
	return &ParseError{
		Pos:     pos,
		Message: message,
		Context: extractContext(context, pos),
	}
}

// NewMalformedTagError creates a new MalformedTagError.
func NewMalformedTagError(pos Position, tagName, message, context string) *MalformedTagError {
	return &MalformedTagError{
		ParseError: ParseError{
			Pos:     pos,
			Message: message,
			Context: extractContext(context, pos),
		},
		TagName: tagName,
	}
}

// NewAttributeParsingError creates a new AttributeParsingError.
func NewAttributeParsingError(pos Position, tagName, attrName, message, context string) *AttributeParsingError {
	return &AttributeParsingError{
		ParseError: ParseError{
			Pos:     pos,
			Message: message,
			Context: extractContext(context, pos),
		},
		TagName:       tagName,
		AttributeName: attrName,
	}
}

// NewUnmatchedTagError creates a new UnmatchedTagError.
func NewUnmatchedTagError(pos Position, tagName, context string) *UnmatchedTagError {
	return &UnmatchedTagError{
		ParseError: ParseError{
			Pos:     pos,
			Message: "closing tag has no matching opening tag",
			Context: extractContext(context, pos),
		},
		TagName: tagName,
	}
}

// NewValidationError creates a new ValidationError.
func NewValidationError(pos Position, sectionName, message, context string) *ValidationError {
	return &ValidationError{
		ParseError: ParseError{
			Pos:     pos,
			Message: message,
			Context: extractContext(context, pos),
		},
		SectionName: sectionName,
	}
}

// extractContext extracts a snippet of text around the error position for context.
// It tries to include a few lines before and after the error.
func extractContext(content string, pos Position) string {
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")
	if pos.Line > len(lines) {
		return content // Fallback if position is out of range
	}

	// Determine the range of lines to include
	startLine := max(0, pos.Line-3)
	endLine := min(len(lines)-1, pos.Line+1)

	// Build the context with line numbers
	var contextBuilder strings.Builder
	for i := startLine; i <= endLine; i++ {
		lineNum := i + 1 // Convert to 1-based line number
		if lineNum == pos.Line {
			// Highlight the error line
			contextBuilder.WriteString(fmt.Sprintf("-> %d: %s\n", lineNum, lines[i]))

			// Add a pointer to the column if possible
			if pos.Column <= len(lines[i])+1 {
				contextBuilder.WriteString(strings.Repeat(" ", pos.Column+5) + "^\n")
			}
		} else {
			contextBuilder.WriteString(fmt.Sprintf("   %d: %s\n", lineNum, lines[i]))
		}
	}

	return contextBuilder.String()
}

// Helper functions
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a > b {
		return b
	}
	return a
}
