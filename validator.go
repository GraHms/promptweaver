package promptweaver

import (
	"fmt"
	"regexp"
)

// Validator is an interface for validating section content.
type Validator interface {
	// Validate checks if the content is valid.
	// Returns nil if valid, or an error if invalid.
	Validate(sectionName string, content string, pos Position) error
}

// RegexValidator validates content against a regular expression.
type RegexValidator struct {
	Pattern     *regexp.Regexp
	Description string // Human-readable description of what the pattern expects
}

// Validate implements the Validator interface.
func (v *RegexValidator) Validate(sectionName string, content string, pos Position) error {
	if !v.Pattern.MatchString(content) {
		return NewValidationError(
			pos,
			sectionName,
			fmt.Sprintf("content does not match expected pattern: %s", v.Description),
			content,
		)
	}
	return nil
}

// FuncValidator uses a custom function to validate content.
type FuncValidator struct {
	ValidateFunc func(sectionName string, content string, pos Position) error
}

// Validate implements the Validator interface.
func (v *FuncValidator) Validate(sectionName string, content string, pos Position) error {
	return v.ValidateFunc(sectionName, content, pos)
}

// ValidatorRegistry manages validators for different section types.
type ValidatorRegistry struct {
	validators map[string][]Validator
}

// NewValidatorRegistry creates a new validator registry.
func NewValidatorRegistry() *ValidatorRegistry {
	return &ValidatorRegistry{
		validators: make(map[string][]Validator),
	}
}

// Register adds a validator for a section type.
// Multiple validators can be registered for the same section type.
func (r *ValidatorRegistry) Register(sectionName string, validator Validator) {
	if validator == nil {
		return
	}
	sectionName = canonicalName(sectionName)
	r.validators[sectionName] = append(r.validators[sectionName], validator)
}

// RegisterRegex creates and registers a RegexValidator.
func (r *ValidatorRegistry) RegisterRegex(sectionName, pattern, description string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid regex pattern for section %s: %w", sectionName, err)
	}

	r.Register(sectionName, &RegexValidator{
		Pattern:     re,
		Description: description,
	})
	return nil
}

// RegisterFunc creates and registers a FuncValidator.
func (r *ValidatorRegistry) RegisterFunc(sectionName string, validateFunc func(string, string, Position) error) {
	r.Register(sectionName, &FuncValidator{
		ValidateFunc: validateFunc,
	})
}

// ValidateSection validates content for a section type.
// Returns nil if valid, or an error if any validator fails.
func (r *ValidatorRegistry) ValidateSection(sectionName string, content string, pos Position) error {
	sectionName = canonicalName(sectionName)
	validators, ok := r.validators[sectionName]
	if !ok {
		// No validators registered for this section type
		return nil
	}

	for _, validator := range validators {
		if err := validator.Validate(sectionName, content, pos); err != nil {
			return err
		}
	}

	return nil
}

// Helper function to normalize section names
func canonicalName(name string) string {
	return name // For now, just return as is; could add case normalization if needed
}
