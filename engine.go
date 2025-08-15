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
type Engine struct{ reg *Registry }

func NewEngine(reg *Registry) *Engine { return &Engine{reg: reg} }

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

	p := newParser(e.reg, sink)
	buf := make([]byte, 4096)
	for {
		n, readErr := br.Read(buf)
		if n > 0 {
			p.feed(buf[:n])
			if err := p.drain(); err != nil {
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

type parser struct {
	reg    *Registry
	sink   *HandlerSink
	buf    bytes.Buffer // rolling buffer of unconsumed bytes
	active *element     // currently open recognized section, or nil
}

type element struct {
	name  string // original open tag name as seen in stream (e.g., "create-file")
	canon string // canonical name if recognized (e.g., "write-file"); empty if unknown
	attrs map[string]string
	body  strings.Builder
}

func newParser(reg *Registry, sink *HandlerSink) *parser { return &parser{reg: reg, sink: sink} }

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
			consumed, isClose, complete := p.parseOwnClose(data)
			if !complete {
				// Need more bytes to decide
				return nil
			}
			if isClose {
				// Consume the closing tag and emit the section event
				p.consume(consumed)
				ev := SectionEvent{
					Name:    p.active.canon,
					Attrs:   p.active.attrs,
					Content: p.active.body.String(),
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
		consumed, tok, ok := parseTagToken(data)
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
		}
	}
}

// parseOwnClose checks whether data starts with a closing tag that should close p.active.
// Accepts any alias whose canonical equals p.active.canon.
// Returns (consumedBytes, isOurClose, complete).

func (p *parser) parseOwnClose(data []byte) (int, bool, bool) {
	if p.active == nil {
		return 0, false, true
	}
	if len(data) < 2 || data[0] != '<' || data[1] != '/' {
		return 0, false, true
	}
	i := 2
	// NEW: tolerate whitespace after "</"
	for i < len(data) && isSpace(data[i]) {
		i++
	}
	if i == len(data) {
		return 0, false, false
	}

	start := i
	for i < len(data) && isNameChar(data[i]) {
		i++
	}
	if i == start { // no name
		return 0, false, true
	}
	if i == len(data) { // incomplete closer across chunk
		return 0, false, false
	}

	closeName := strings.ToLower(string(data[start:i]))

	// Accept if canonical(closeName) == active.canon
	if c, ok := p.reg.Canonical(closeName); ok {
		if c != p.active.canon {
			return 0, false, true
		}
	} else {
		// Fallback: literal match against the original open tag name (case-insensitive)
		if !strings.EqualFold(closeName, p.active.name) {
			return 0, false, true
		}
	}

	// optional spaces before '>'
	for i < len(data) && isSpace(data[i]) {
		i++
	}
	if i == len(data) {
		return 0, false, false
	}
	if data[i] != '>' {
		return 0, false, true
	}

	return i + 1, true, true
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
		p.sink.Emit(SectionEvent{
			Name:    p.active.canon,
			Attrs:   p.active.attrs,
			Content: p.active.body.String(),
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

func (p *parser) consume(n int) { _ = p.buf.Next(n) }

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
// Returns (consumedBytes, token, ok). If ok=false, the caller should wait for more input.
func parseTagToken(data []byte) (int, tagToken, bool) {
	if len(data) == 0 || data[0] != '<' {
		return 0, tagToken{}, false
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
			return 0, tagToken{}, false
		}
		name := string(data[start:i])
		skipSpaces()
		if i == len(data) || data[i] != '>' {
			return 0, tagToken{}, false
		}
		return i + 1, tagToken{kind: tokenClose, name: name}, true
	}

	// Opening or self-closing
	start := i
	for i < len(data) && isNameChar(data[i]) {
		i++
	}
	if i == len(data) {
		return 0, tagToken{}, false
	}
	name := string(data[start:i])

	attrs := map[string]string{}
	for {
		skipSpaces()
		if i == len(data) {
			return 0, tagToken{}, false
		}

		switch data[i] {
		case '>':
			return i + 1, tagToken{kind: tokenOpen, name: name, attrs: attrs}, true
		case '/':
			i++
			if i == len(data) || data[i] != '>' {
				return 0, tagToken{}, false
			}
			return i + 1, tagToken{kind: tokenSelfClose, name: name, attrs: attrs}, true
		}

		// attribute key
		kStart := i
		for i < len(data) && isAttrNameChar(data[i]) {
			i++
		}
		if i == len(data) {
			return 0, tagToken{}, false
		}
		key := string(data[kStart:i])

		skipSpaces()
		if i == len(data) || data[i] != '=' {
			return 0, tagToken{}, false
		}
		i++
		skipSpaces()
		if i == len(data) {
			return 0, tagToken{}, false
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
				return 0, tagToken{}, false
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
						return 0, tagToken{}, false
					}
					i++ // consume closing quote
				default:
					i++
				}
			}
			if depth != 0 {
				return 0, tagToken{}, false
			} // incomplete
			val := string(data[vStart : i-1]) // without outer braces
			attrs[strings.ToLower(strings.TrimSpace(key))] = "{" + val + "}"

		default:
			// unsupported value form (bareword) → treat as incomplete to wait for more bytes
			return 0, tagToken{}, false
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
