package promptweaver

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
)

// SectionPlugin declares a tag name that the engine should recognize and emit.
type SectionPlugin struct {
	Name    string
	Aliases []string
}

// SectionEvent is emitted when a registered section is closed (or a self-closing tag is parsed).
type SectionEvent struct {
	Name    string            // section/tag name
	Attrs   map[string]string // parsed attributes on the opening tag
	Content string            // inner text content between <tag> and </tag>
}

// Registry holds enabled section names. It maps aliases -> canonical name.
type Registry struct{ canon map[string]string }

func NewRegistry() *Registry { return &Registry{canon: map[string]string{}} }
func (r *Registry) Register(p SectionPlugin) {
	if p.Name == "" {
		return
	}
	canon := strings.ToLower(p.Name)
	r.canon[canon] = canon
	for _, a := range p.Aliases {
		if a == "" {
			continue
		}
		r.canon[strings.ToLower(a)] = canon
	}
}
func (r *Registry) IsAllowed(name string) bool { _, ok := r.canon[strings.ToLower(name)]; return ok }
func (r *Registry) Canonical(name string) (string, bool) {
	c, ok := r.canon[strings.ToLower(name)]
	return c, ok
}

// HandlerSink routes events to handlers registered per section name.
type HandlerSink struct{ handlers map[string]func(SectionEvent) }

func NewHandlerSink() *HandlerSink { return &HandlerSink{handlers: map[string]func(SectionEvent){}} }
func (s *HandlerSink) RegisterHandler(section string, fn func(SectionEvent)) {
	if section == "" || fn == nil {
		return
	}
	s.handlers[strings.ToLower(section)] = fn
}
func (s *HandlerSink) Emit(ev SectionEvent) {
	if fn, ok := s.handlers[strings.ToLower(ev.Name)]; ok {
		fn(ev)
	}
}

// Engine coordinates streaming parsing and event emission.
type Engine struct {
	reg        *Registry
	options    EngineOptions
	validators *ValidatorRegistry
}

// NewEngine creates a new Engine with the given registry and default options.
func NewEngine(reg *Registry) *Engine {
	return NewEngineWithOptions(reg, DefaultEngineOptions())
}

// NewEngineWithOptions creates a new Engine with the given registry and options.
func NewEngineWithOptions(reg *Registry, options EngineOptions) *Engine {
	return &Engine{
		reg:        reg,
		options:    options,
		validators: NewValidatorRegistry(),
	}
}

// RegisterValidator registers a validator for a section type.
func (e *Engine) RegisterValidator(sectionName string, validator Validator) {
	e.validators.Register(sectionName, validator)
}

// RegisterRegexValidator creates and registers a regex validator.
func (e *Engine) RegisterRegexValidator(sectionName, pattern, description string) error {
	return e.validators.RegisterRegex(sectionName, pattern, description)
}

// RegisterFuncValidator creates and registers a function validator.
func (e *Engine) RegisterFuncValidator(sectionName string, validateFunc func(string, string, Position) error) {
	e.validators.RegisterFunc(sectionName, validateFunc)
}

// ProcessStream incrementally parses from r and emits SectionEvents to sink as soon as sections close.
// The format is a resilient XML-lite with rules:
//   - Opening tag:   <name attr="value" attr2='v'>
//   - Closing tag:   </name>
//   - Self-closing:  <name .../>
//   - Text nodes are treated as raw content. Nesting is supported; only registered tags produce events.
func (e *Engine) ProcessStream(r io.Reader, sink *HandlerSink) error {
	if e.reg == nil {
		return errors.New("nil registry")
	}
	br := bufio.NewReader(r)

	p := newParser(e.reg, sink, e.options)
	p.validators = e.validators // Pass validators to the parser

	buf := make([]byte, 4096)
	for {
		n, readErr := br.Read(buf)
		if n > 0 {
			p.feed(buf[:n])
			if err := p.drain(); err != nil {
				// If a custom error handler is provided, use it
				if p.errorHandler != nil {
					if p.errorHandler(err) {
						// Handler returned true, continue parsing
						continue
					}
					// Handler returned false, stop parsing
					return err
				}

				// No custom handler, use recovery mode
				if e.options.RecoveryMode == ContinueMode {
					// In a real implementation, we might use a logger here
					// For now, we'll just continue
					continue
				}
				return err
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return p.finish()
			}
			return readErr
		}
	}
}

// --- Streaming parser implementation ---

// --- Streaming parser implementation (flat / non-nested) ---

// RecoveryMode defines how the parser should handle errors.
type RecoveryMode int

const (
	// StrictMode stops parsing on the first error.
	StrictMode RecoveryMode = iota

	// ContinueMode attempts to recover from errors and continue parsing.
	ContinueMode
)

// ErrorHandler is a function that can process parsing errors.
// It receives the error and can decide whether to continue parsing.
// If it returns true, parsing will continue; if false, parsing will stop.
type ErrorHandler func(error) bool

// EngineOptions configures the behavior of the Engine.
type EngineOptions struct {
	// RecoveryMode determines how the parser handles errors.
	// Default is StrictMode.
	RecoveryMode RecoveryMode

	// ErrorHandler is called when a parsing error occurs.
	// If nil, the default behavior is used based on RecoveryMode.
	// If provided, it can override the RecoveryMode behavior.
	ErrorHandler ErrorHandler
}

// DefaultEngineOptions returns the default engine options.
func DefaultEngineOptions() EngineOptions {
	return EngineOptions{
		RecoveryMode: StrictMode,
		ErrorHandler: nil, // Default to nil, will use RecoveryMode behavior
	}
}

// WithContinueMode returns engine options configured for continue mode.
func WithContinueMode() EngineOptions {
	return EngineOptions{
		RecoveryMode: ContinueMode,
		ErrorHandler: nil,
	}
}

// WithErrorHandler returns engine options with a custom error handler.
func WithErrorHandler(handler ErrorHandler) EngineOptions {
	return EngineOptions{
		RecoveryMode: StrictMode, // Default to strict, but handler can override
		ErrorHandler: handler,
	}
}

type parser struct {
	reg          *Registry
	sink         *HandlerSink
	buf          bytes.Buffer       // rolling buffer of unconsumed bytes
	active       *element           // currently open recognized section, or nil
	pos          Position           // current position in the input stream
	recoveryMode RecoveryMode       // how to handle errors
	errorHandler ErrorHandler       // custom error handler
	validators   *ValidatorRegistry // content validators
	lastContent  string             // recent content for error context
}

type element struct {
	name  string // original open tag name as seen in stream (e.g., "create-file")
	canon string // canonical name if recognized (e.g., "write-file"); empty if unknown
	attrs map[string]string
	body  strings.Builder
}

func newParser(reg *Registry, sink *HandlerSink, options EngineOptions) *parser {
	return &parser{
		reg:          reg,
		sink:         sink,
		pos:          Position{Line: 1, Column: 1}, // Start at line 1, column 1
		recoveryMode: options.RecoveryMode,
		errorHandler: options.ErrorHandler,
	}
}

func (p *parser) feed(b []byte) { p.buf.Write(b) }

// drain consumes as much of p.buf as possible.
// Flat mode: if a recognized tag is open, treat all inner bytes as text until its matching </...>.
func (p *parser) drain() error {
	for {
		data := p.buf.Bytes()
		if len(data) == 0 {
			return nil
		}

		// If we are inside a recognized section, stream raw until its close.
		if p.active != nil {
			// Write everything up to the next '<' (if any)
			lt := bytes.IndexByte(data, '<')
			if lt == -1 {
				// No '<' at all → dump everything as content
				p.active.body.Write(data)
				p.consume(len(data))
				continue
			}
			if lt > 0 {
				// Write text before '<'
				p.active.body.Write(data[:lt])
				p.consume(lt)
				continue
			}

			// Now data[0] == '<' — it *might* be our closing tag.
			// Only close if it’s exactly a recognized close for this active section (by alias/canonical).
			consumed, isClose, complete, err := p.parseOwnClose(data)
			if err != nil {
				// Error parsing closing tag
				if p.recoveryMode == ContinueMode {
					// In recovery mode, consume the bytes up to the error and continue
					p.consume(consumed)
					continue
				}
				return err
			}
			if !complete {
				// Need more bytes to decide
				return nil
			}
			if isClose {
				// Consume the closing tag
				p.consume(consumed)

				// Prepare the section event
				content := p.active.body.String()
				sectionName := p.active.canon

				// Validate the section content if validators are available
				if p.validators != nil {
					if err := p.validators.ValidateSection(sectionName, content, p.pos); err != nil {
						// Handle validation error
						if p.errorHandler != nil {
							if p.errorHandler(err) {
								// Handler returned true, continue with next section
								p.active = nil
								continue
							}
							// Handler returned false, stop parsing
							return err
						}

						// No custom handler, use recovery mode
						if p.recoveryMode == StrictMode {
							return err
						}
						// In ContinueMode, just skip this section and continue
						p.active = nil
						continue
					}
				}

				// Content is valid or no validators, emit the event
				ev := SectionEvent{
					Name:    sectionName,
					Attrs:   p.active.attrs,
					Content: content,
				}
				p.active = nil
				p.sink.Emit(ev)
				continue
			}

			// Not our closing tag → treat leading '<' as literal text
			// (Optional: if the next chars are "</", consume both; otherwise just consume '<')
			if len(data) >= 2 && data[1] == '/' {
				p.active.body.WriteString("</")
				p.consume(2)
			} else {
				p.active.body.WriteByte('<')
				p.consume(1)
			}
			continue
		}

		// No active section: look for a tag opener
		lt := bytes.IndexByte(data, '<')
		if lt == -1 {
			// Text outside any tag is ignored
			p.buf.Reset()
			return nil
		}
		if lt > 0 {
			// Ignore preceding text
			p.consume(lt)
			continue
		}

		// data[0] == '<' — try to parse a tag token
		consumed, tok, ok, err := parseTagToken(data, p.pos, p.lastContent)
		if err != nil {
			// Error parsing tag
			if p.recoveryMode == ContinueMode {
				// In recovery mode, consume the bytes up to the error and continue
				p.consume(consumed)
				continue
			}
			return err
		}
		if !ok {
			// Need more bytes to complete tag
			return nil
		}
		p.consume(consumed)

		switch tok.kind {
		case tokenOpen:
			if c, ok := p.reg.Canonical(tok.name); ok {
				// Start flat (raw) mode for this section
				p.active = &element{name: tok.name, canon: c, attrs: tok.attrs}
			} else {
				// Unknown tag outside sections → ignore it (and its contents are ignored too,
				// because we never enter active mode for unknowns)
			}

		case tokenSelfClose:
			if c, ok := p.reg.Canonical(tok.name); ok {
				p.sink.Emit(SectionEvent{Name: c, Attrs: tok.attrs, Content: ""})
			} // else ignore

		case tokenClose:
			// Closing tag with no active section → ignore
			// In strict mode, we could report this as an error
			if p.recoveryMode == StrictMode {
				return NewUnmatchedTagError(p.pos, tok.name, p.lastContent)
			}
		}
	}
}

// parseOwnClose checks whether data starts with a closing tag that should close p.active.
// Accepts any alias whose canonical equals p.active.canon.
// Returns (consumedBytes, isOurClose, complete, error).

func (p *parser) parseOwnClose(data []byte) (int, bool, bool, error) {
	if p.active == nil {
		return 0, false, true, nil
	}
	if len(data) < 2 || data[0] != '<' || data[1] != '/' {
		return 0, false, true, nil
	}
	i := 2
	// Tolerate whitespace after "</"
	for i < len(data) && isSpace(data[i]) {
		i++
	}
	if i == len(data) {
		return 0, false, false, nil
	}

	start := i
	for i < len(data) && isNameChar(data[i]) {
		i++
	}
	if i == start { // no name
		if p.recoveryMode == StrictMode {
			return i, false, true, NewMalformedTagError(
				p.pos, "", "missing tag name after '</'", p.lastContent)
		}
		return 0, false, true, nil
	}
	if i == len(data) { // incomplete closer across chunk
		return 0, false, false, nil
	}

	closeName := strings.ToLower(string(data[start:i]))

	// Accept if canonical(closeName) == active.canon
	if c, ok := p.reg.Canonical(closeName); ok {
		if c != p.active.canon {
			// Not our closing tag, but a valid tag name
			return 0, false, true, nil
		}
	} else {
		// Fallback: literal match against the original open tag name (case-insensitive)
		if !strings.EqualFold(closeName, p.active.name) {
			// Not our closing tag
			return 0, false, true, nil
		}
	}

	// optional spaces before '>'
	for i < len(data) && isSpace(data[i]) {
		i++
	}
	if i == len(data) {
		return 0, false, false, nil
	}
	if data[i] != '>' {
		if p.recoveryMode == StrictMode {
			return i, false, true, NewMalformedTagError(
				p.pos, closeName, "expected '>' after closing tag name", p.lastContent)
		}
		return 0, false, true, nil
	}

	return i + 1, true, true, nil
}

func (p *parser) finish() error {
	// If buffer has leftover bytes, and we are inside a section, they are part of the content.
	if p.buf.Len() > 0 && p.active != nil {
		p.active.body.Write(p.buf.Bytes())
		p.buf.Reset()
	} else {
		p.buf.Reset()
	}

	// Auto-close active recognized section on EOF
	if p.active != nil && p.active.canon != "" {
		content := p.active.body.String()
		sectionName := p.active.canon

		// Validate the section content if validators are available
		if p.validators != nil {
			if err := p.validators.ValidateSection(sectionName, content, p.pos); err != nil {
				// Handle validation error
				if p.errorHandler != nil {
					if !p.errorHandler(err) {
						// Handler returned false, stop parsing
						return err
					}
					// Handler returned true, continue and emit anyway
				} else if p.recoveryMode == StrictMode {
					return err
				}
				// In ContinueMode or if handler returned true, emit anyway
			}
		}

		// Emit the section event
		p.sink.Emit(SectionEvent{
			Name:    sectionName,
			Attrs:   p.active.attrs,
			Content: content,
		})
		p.active = nil
	}
	return nil
}

func (p *parser) appendText(s string) {
	// In flat mode, we only append when an active section exists.
	if p.active == nil || s == "" {
		return
	}
	p.active.body.WriteString(s)
}

// consume processes n bytes from the buffer, updating position tracking
func (p *parser) consume(n int) {
	// Store the consumed bytes for context in error messages
	consumed := p.buf.Bytes()[:n]
	p.updateLastContent(string(consumed))

	// Update line and column positions
	for i := 0; i < n; i++ {
		if i < len(consumed) && consumed[i] == '\n' {
			p.pos.Line++
			p.pos.Column = 1
		} else {
			p.pos.Column++
		}
	}

	// Remove the bytes from the buffer
	_ = p.buf.Next(n)
}

// updateLastContent maintains a sliding window of recent content for error context
func (p *parser) updateLastContent(s string) {
	const maxContextLen = 1000 // Limit context size to avoid memory issues
	p.lastContent += s
	if len(p.lastContent) > maxContextLen {
		p.lastContent = p.lastContent[len(p.lastContent)-maxContextLen:]
	}
}

// --- Tag tokenization ---

type tagTokenKind int

const (
	tokenOpen tagTokenKind = iota
	tokenClose
	tokenSelfClose
)

type tagToken struct {
	kind  tagTokenKind
	name  string
	attrs map[string]string
}

// parseTagToken tries to parse a single tag token from the beginning of data (which must start with '<').
// Returns (consumedBytes, token, ok, error). If ok=false and error is nil, the caller should wait for more input.
// If error is not nil, parsing failed with a specific error.
func parseTagToken(data []byte, pos Position, context string) (int, tagToken, bool, error) {
	if len(data) == 0 || data[0] != '<' {
		return 0, tagToken{}, false, nil
	}

	i := 1
	skipSpaces := func() {
		for i < len(data) && isSpace(data[i]) {
			i++
		}
	}

	// Closing tag?
	if i < len(data) && data[i] == '/' {
		i++
		start := i
		for i < len(data) && isNameChar(data[i]) {
			i++
		}
		if i == len(data) {
			return 0, tagToken{}, false, nil
		}
		name := string(data[start:i])
		skipSpaces()
		if i == len(data) {
			return 0, tagToken{}, false, nil
		}
		if data[i] != '>' {
			return i, tagToken{}, false, NewMalformedTagError(
				pos, name, "expected '>' after closing tag name", context)
		}
		return i + 1, tagToken{kind: tokenClose, name: name}, true, nil
	}

	// Opening or self-closing
	start := i
	for i < len(data) && isNameChar(data[i]) {
		i++
	}
	if i == len(data) {
		return 0, tagToken{}, false, nil
	}
	if start == i {
		return i, tagToken{}, false, NewMalformedTagError(
			pos, "", "missing tag name after '<'", context)
	}
	name := string(data[start:i])

	attrs := map[string]string{}
	for {
		skipSpaces()
		if i == len(data) {
			return 0, tagToken{}, false, nil
		}

		switch data[i] {
		case '>':
			return i + 1, tagToken{kind: tokenOpen, name: name, attrs: attrs}, true, nil
		case '/':
			i++
			if i == len(data) {
				return 0, tagToken{}, false, nil
			}
			if data[i] != '>' {
				return i, tagToken{}, false, NewMalformedTagError(
					pos, name, "expected '>' after '/' in self-closing tag", context)
			}
			return i + 1, tagToken{kind: tokenSelfClose, name: name, attrs: attrs}, true, nil
		}

		// attribute key
		kStart := i
		for i < len(data) && isAttrNameChar(data[i]) {
			i++
		}
		if i == len(data) {
			return 0, tagToken{}, false, nil
		}
		if kStart == i {
			return i, tagToken{}, false, NewMalformedTagError(
				pos, name, "expected attribute name or '>' or '/>'", context)
		}
		key := string(data[kStart:i])

		skipSpaces()
		if i == len(data) {
			return 0, tagToken{}, false, nil
		}
		if data[i] != '=' {
			return i, tagToken{}, false, NewAttributeParsingError(
				pos, name, key, "expected '=' after attribute name", context)
		}
		i++
		skipSpaces()
		if i == len(data) {
			return 0, tagToken{}, false, nil
		}

		// attribute value: quoted "…"/'…' OR JSX braced { … }
		switch data[i] {
		case '"', '\'':
			quote := data[i]
			i++
			vStart := i
			for i < len(data) && data[i] != quote {
				if data[i] == '\\' && i+1 < len(data) { // skip escapes
					i += 2
					continue
				}
				i++
			}
			if i == len(data) {
				return 0, tagToken{}, false, nil
			}
			val := string(data[vStart:i])
			i++ // consume closing quote
			attrs[strings.ToLower(strings.TrimSpace(key))] = val

		case '{':
			// scan balanced braces, allowing nested { } and quoted strings inside
			i++
			vStart := i
			depth := 1
			for i < len(data) && depth > 0 {
				switch data[i] {
				case '{':
					depth++
					i++
				case '}':
					depth--
					i++
				case '"', '\'':
					q := data[i]
					i++
					for i < len(data) && data[i] != q {
						if data[i] == '\\' && i+1 < len(data) {
							i += 2
							continue
						}
						i++
					}
					if i == len(data) {
						return 0, tagToken{}, false, nil
					}
					i++ // consume closing quote
				default:
					i++
				}
			}
			if depth != 0 {
				return 0, tagToken{}, false, nil
			} // incomplete
			val := string(data[vStart : i-1]) // without outer braces
			attrs[strings.ToLower(strings.TrimSpace(key))] = "{" + val + "}"

		default:
			return i, tagToken{}, false, NewAttributeParsingError(
				pos, name, key, "expected attribute value to start with quote or brace", context)
		}
	}
}

func isSpace(b byte) bool { return b == ' ' || b == '\n' || b == '\t' || b == '\r' }
func isNameChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-'
}
func isAttrNameChar(b byte) bool { return isNameChar(b) }

func matchIndex(stack []*element, closeName string, reg *Registry) int {
	// Prefer canonical/alias match if recognized
	if c, ok := reg.Canonical(closeName); ok {
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].canon == c {
				return i
			}
		}
		return -1
	}
	// Fallback: match by original name (for unknown inner markup like </div>)
	for i := len(stack) - 1; i >= 0; i-- {
		if strings.EqualFold(stack[i].name, closeName) {
			return i
		}
	}
	return -1
}

func attrsToString(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	var b strings.Builder
	first := true
	for k, v := range m {
		if !first {
			b.WriteByte(' ')
		} else {
			first = false
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteByte('"')
		b.WriteString(strings.ReplaceAll(v, "\"", "&quot;"))
		b.WriteByte('"')
	}
	return b.String()
}

func looksLikeOwnClose(data []byte, openName string) (bool, bool) {
	// returns (isCloseForThis, complete)
	if len(data) < 3 || data[0] != '<' || data[1] != '/' {
		// not even a closing tag
		return false, true
	}
	// Need enough bytes to compare the name
	if len(data) < 2+len(openName)+1 { // + '>' at least
		return false, false // incomplete
	}
	// Compare name literally after "</"
	if !strings.HasPrefix(string(data[2:]), openName) {
		return false, true
	}
	j := 2 + len(openName)
	// allow spaces before '>'
	for j < len(data) && isSpace(data[j]) {
		j++
	}
	if j < len(data) && data[j] == '>' {
		return true, true
	}
	// maybe incomplete (e.g., boundary right before '>')
	return false, false
}

// ReaderFromString is a helper to turn strings into an io.Reader for tests/examples.
func ReaderFromString(s string) io.Reader { return strings.NewReader(s) }
