package promptweaver

import (
	"strings"
	"testing"
)

func Test_Engine_Should_Report_AttributeParsingError(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "think"})
	sink := NewHandlerSink()

	// Use strict mode (default)
	en := NewEngine(reg)
	// Use a malformed attribute without a value
	input := `<think attr></think>`
	err := en.ProcessStream(ReaderFromString(input), sink)

	if err == nil {
		t.Fatal("expected error for malformed attribute, got nil")
	}

	// Check that it's the right error type
	attrErr, ok := err.(*AttributeParsingError)
	if !ok {
		t.Fatalf("expected AttributeParsingError, got %T: %v", err, err)
	}

	// Check error details
	if attrErr.TagName != "think" {
		t.Errorf("expected tag name 'think', got %q", attrErr.TagName)
	}
	if attrErr.AttributeName != "attr" {
		t.Errorf("expected attribute name 'attr', got %q", attrErr.AttributeName)
	}
}

func Test_Engine_Should_Continue_After_Error_In_ContinueMode(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "think"})
	reg.Register(SectionPlugin{Name: "summary"})

	var events []SectionEvent
	sink := NewHandlerSink()
	sink.RegisterHandler("think", func(ev SectionEvent) {
		events = append(events, ev)
	})
	sink.RegisterHandler("summary", func(ev SectionEvent) {
		events = append(events, ev)
	})

	// Use continue mode
	en := NewEngineWithOptions(reg, WithContinueMode())
	// Use a malformed tag followed by a valid tag
	input := `<think>First</think></bogus><summary>Last</summary>`
	err := en.ProcessStream(ReaderFromString(input), sink)

	// Should not return an error
	if err != nil {
		t.Fatalf("expected no error in continue mode, got: %v", err)
	}

	// Should have processed the first and last sections
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if events[0].Name != "think" || events[0].Content != "First" {
		t.Errorf("unexpected first event: %+v", events[0])
	}

	if events[1].Name != "summary" || events[1].Content != "Last" {
		t.Errorf("unexpected last event: %+v", events[1])
	}
}

func Test_Engine_Should_Use_Custom_ErrorHandler(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "think"})

	var events []SectionEvent
	sink := NewHandlerSink()
	sink.RegisterHandler("think", func(ev SectionEvent) {
		events = append(events, ev)
	})

	// Track errors
	var handledErrors []error
	errorHandler := func(err error) bool {
		handledErrors = append(handledErrors, err)
		return true // continue parsing
	}

	// Use custom error handler
	en := NewEngineWithOptions(reg, WithErrorHandler(errorHandler))
	input := `<think>Good</think></bogus>`
	err := en.ProcessStream(ReaderFromString(input), sink)

	// Should not return an error because handler returns true
	if err != nil {
		t.Fatalf("expected no error with custom handler, got: %v", err)
	}

	// Should have processed the good section
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Should have handled one error
	if len(handledErrors) != 1 {
		t.Fatalf("expected 1 handled error, got %d", len(handledErrors))
	}

	// Check error type
	_, ok := handledErrors[0].(*UnmatchedTagError)
	if !ok {
		t.Errorf("expected UnmatchedTagError, got %T", handledErrors[0])
	}
}

func Test_Engine_Should_Validate_Section_Content(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "code"})

	var events []SectionEvent
	sink := NewHandlerSink()
	sink.RegisterHandler("code", func(ev SectionEvent) {
		events = append(events, ev)
	})

	// Create engine with validator
	en := NewEngine(reg)

	// Register a validator that requires "func" in code sections
	err := en.RegisterRegexValidator("code", "func", "must contain a function definition")
	if err != nil {
		t.Fatalf("failed to register validator: %v", err)
	}

	// Valid content
	input1 := `<code>func main() {}</code>`
	err = en.ProcessStream(ReaderFromString(input1), sink)
	if err != nil {
		t.Fatalf("unexpected error for valid content: %v", err)
	}

	// Invalid content
	input2 := `<code>var x = 1;</code>`
	err = en.ProcessStream(ReaderFromString(input2), sink)

	// Should return validation error
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	// Check error type
	validationErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}

	// Check error details
	if validationErr.SectionName != "code" {
		t.Errorf("expected section name 'code', got %q", validationErr.SectionName)
	}
	if !strings.Contains(validationErr.Error(), "must contain a function") {
		t.Errorf("error message doesn't include validation description: %s", validationErr.Error())
	}

	// Should have processed only the valid section
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func Test_Engine_Should_Support_Custom_Validation_Functions(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "json"})

	var events []SectionEvent
	sink := NewHandlerSink()
	sink.RegisterHandler("json", func(ev SectionEvent) {
		events = append(events, ev)
	})

	// Create engine
	en := NewEngine(reg)

	// Register a custom validator that checks for valid JSON
	en.RegisterFuncValidator("json", func(sectionName, content string, pos Position) error {
		if !strings.Contains(content, "{") || !strings.Contains(content, "}") {
			return NewValidationError(pos, sectionName, "must contain valid JSON object", content)
		}
		return nil
	})

	// Valid content
	input1 := `<json>{"name": "test"}</json>`
	err := en.ProcessStream(ReaderFromString(input1), sink)
	if err != nil {
		t.Fatalf("unexpected error for valid JSON: %v", err)
	}

	// Invalid content
	input2 := `<json>invalid json</json>`
	err = en.ProcessStream(ReaderFromString(input2), sink)

	// Should return validation error
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	// Check error type
	_, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}

	// Should have processed only the valid section
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}
