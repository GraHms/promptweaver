package promptweaver

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
)

func NewEngine(reg *Registry, opts ...func(*Engine)) *Engine {
	e := &Engine{reg: reg, policy: UnknownDrop}
	for _, o := range opts {
		o(e)
	}
	return e
}
func WithUnknownPolicy(p UnknownTagPolicy) func(*Engine) {
	return func(e *Engine) { e.policy = p }
}

type Event interface{ isEvent() }

type CreateFileEvent struct {
	Path string
}

// WriteFileEvent represents an atomic "create/overwrite and write" operation.
// If Create is true, the executor should ensure the file is created (or truncated) before writing.
type WriteFileEvent struct {
	Path     string
	Language string // optional; useful when body came from a code fence
	Content  string
	Create   bool // true when emitted from <CreateFile> with content
}

func (WriteFileEvent) isEvent() {}

func (CreateFileEvent) isEvent() {}

type EditFileEvent struct {
	Path string
}

func (EditFileEvent) isEvent() {}

type SectionEvent struct {
	Name     string // e.g., Thinking, Analysis, Planning
	Content  string
	Metadata map[string]string
}

func (SectionEvent) isEvent() {}

type CodeBlockEvent struct {
	Name     string
	Language string // e.g., tsx, go, bash
	File     string // optional; from file="..."
	Content  string
}

func (CodeBlockEvent) isEvent() {}

// ===== Sink =====

type EventSink interface {
	OnEvent(ev Event)
}

type EventSinkFunc func(ev Event)

func (f EventSinkFunc) OnEvent(ev Event) { f(ev) }

// ===== Plugins =====

type Plugin interface {
	// Names returns XML tag names handled by this plugin (e.g., ["CreateFile"]).
	Names() []string
	// HandleXMLElement is called with the full XML start.end or self-closed element string.
	HandleXMLElement(raw string, dec *xml.Decoder, start xml.StartElement, sink EventSink) error
}

type Registry struct {
	byName map[string]Plugin
}

func NewRegistry() *Registry {
	return &Registry{byName: map[string]Plugin{}}
}

func (r *Registry) Register(p Plugin) {
	for _, n := range p.Names() {
		r.byName[strings.ToLower(n)] = p
	}
}

func (r *Registry) get(name string) (Plugin, bool) {
	p, ok := r.byName[strings.ToLower(name)]
	return p, ok
}

// ===== Engine =====

var (

	// Start de uma tag XML com nome [A-Za-z][\w.-]* e atributos até ao '>'.
	xmlStartRe = regexp.MustCompile(`(?s)<([A-Za-z][\w\.\-:]*)\b[^>]*>`)
	// Tag self-close: <Tag .../>
	xmlSelfCloseRe = regexp.MustCompile(`(?s)^<([A-Za-z][\w\.\-:]*)\b[^>]*/\s*>$`)
)

// ProcessStream reads from r in chunks and incrementally emits Events.
// It keeps extracting as long as the buffer contains a complete unit.
// At EOF, it *drains* the buffer by repeatedly calling tryExtract until no progress.
func (e *Engine) ProcessStream(r io.Reader, sink EventSink) error {
	br := bufio.NewReader(r)
	var buf bytes.Buffer

	for {
		chunk := make([]byte, 4096)
		n, err := br.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
			// Extract as much as we can from the current buffer.
			for {
				progress, perr := e.tryExtract(&buf, sink)
				if perr != nil {
					return perr
				}
				if !progress {
					break
				}
			}
		}
		if err == io.EOF {
			// IMPORTANT: fully drain whatever remains after the last read.
			for {
				progress, _ := e.tryExtract(&buf, sink)
				if !progress {
					break
				}
			}
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// 4. Balanced XML: <Tag> ... </Tag>
func (e *Engine) tryExtract(buf *bytes.Buffer, sink EventSink) (bool, error) {
	b := buf.Bytes()
	if len(b) == 0 {
		return false, nil
	}

	// 0) Skip ONLY leading ASCII whitespace
	trim := 0
	for trim < len(b) {
		switch b[trim] {
		case ' ', '\t', '\r', '\n':
			trim++
		default:
			goto afterTrim
		}
	}
afterTrim:
	if trim > 0 {
		buf.Next(trim)
		return true, nil
	}
	b = buf.Bytes()
	if len(b) == 0 {
		return false, nil
	}

	// 1) Backtick code fence: ```<lang> <meta>\n ... ```
	if bytes.HasPrefix(b, []byte("```")) {
		nl := bytes.IndexByte(b, '\n')
		if nl == -1 {
			return false, nil // header incomplete
		}
		header := strings.TrimSpace(string(b[3:nl]))
		var lang, meta string
		if sp := strings.IndexByte(header, ' '); sp >= 0 {
			lang = header[:sp]
			meta = strings.TrimSpace(header[sp+1:])
		} else {
			lang = header
		}

		rest := b[nl+1:]
		closeAt := -1
		scan := rest
		offset := 0
		for {
			eol := bytes.IndexByte(scan, '\n')
			if eol == -1 {
				if bytes.HasPrefix(bytes.TrimLeft(scan, " \t"), []byte("```")) {
					closeAt = offset
					break
				}
				return false, nil
			}
			line := scan[:eol]
			if bytes.HasPrefix(bytes.TrimLeft(line, " \t"), []byte("```")) {
				closeAt = offset
				break
			}
			offset += eol + 1
			scan = scan[eol+1:]
		}

		body := string(rest[:closeAt])
		consumed := (nl + 1) + closeAt
		closingRemainder := rest[closeAt:]
		if eol := bytes.IndexByte(closingRemainder, '\n'); eol >= 0 {
			consumed += eol + 1
		} else {
			consumed = len(b)
		}

		file := parseKeyValue(meta, "file")
		buf.Next(consumed)
		sink.OnEvent(CodeBlockEvent{Language: lang, File: file, Content: body})
		return true, nil
	}

	// 2) Self-closed XML at buffer start: <Tag ... />
	if loc := nextTagEnd(b); loc > 0 {
		first := string(b[:loc])
		if xmlSelfCloseRe.MatchString(first) {
			buf.Next(loc)
			dec := xml.NewDecoder(strings.NewReader(first))
			tok, derr := dec.Token()
			if derr != nil {
				return true, derr
			}
			se, ok := tok.(xml.StartElement)
			if !ok {
				return true, fmt.Errorf("expected StartElement for self-closed tag")
			}
			if p, ok := e.reg.get(se.Name.Local); ok {
				if err := p.HandleXMLElement(first, dec, se, sink); err != nil {
					return true, err
				}
				return true, nil
			}
			return true, nil // unknown tag: ignore
		}
	}

	// 3) Balanced XML block: <Tag> ... </Tag>
	if start := xmlStartRe.FindIndex(b); start != nil && start[0] == 0 {
		name := string(xmlStartRe.FindSubmatch(b)[1])
		endIdx, full := findBalancedXMLElement(b, name)
		if full {
			raw := b[:endIdx]
			buf.Next(endIdx)
			dec := xml.NewDecoder(bytes.NewReader(raw))
			tok, derr := dec.Token()
			if derr != nil {
				return true, derr
			}
			se, ok := tok.(xml.StartElement)
			if !ok {
				return true, fmt.Errorf("expected StartElement")
			}
			if p, ok := e.reg.get(se.Name.Local); ok {
				if err := p.HandleXMLElement(string(raw), dec, se, sink); err != nil {
					return true, err
				}
				return true, nil
			}
			// unknown tag: still emit as SectionEvent if auditing
			if e.policy == UnknownAudit {
				body, _ := readInnerText(dec, se)
				sink.OnEvent(SectionEvent{Name: se.Name.Local, Content: strings.TrimSpace(body)})
			}
			return true, nil
		}
	}

	// 4) Plain text: emit until next tag/fence or EOF
	nextPos := len(b)
	if loc := bytes.Index(b, []byte("```")); loc >= 0 && loc < nextPos {
		nextPos = loc
	}
	if loc := xmlStartRe.FindIndex(b); loc != nil && loc[0] < nextPos {
		nextPos = loc[0]
	}

	nextTag := bytes.IndexByte(b, '<')
	nextFence := bytes.Index(b, []byte("```"))
	nextPos = minPositive(nextTag, nextFence, len(b))

	if nextPos > 0 {
		plain := string(b[:nextPos])
		buf.Next(nextPos)
		sink.OnEvent(SectionEvent{Name: "PlainText", Content: strings.TrimSpace(plain)})
		return true, nil
	}

	return false, nil
}
func minPositive(vals ...int) int {
	min := -1
	for _, v := range vals {
		if v < 0 {
			continue
		}
		if min == -1 || v < min {
			min = v
		}
	}
	if min == -1 {
		return 0 // ou len(buf), dependendo do contexto
	}
	return min
}

// ===== util =====

func parseKeyValue(meta, key string) string {
	// meta: e.g. file="components/button.tsx" other=...
	keyEq := key + "="
	idx := strings.Index(meta, keyEq)
	if idx < 0 {
		return ""
	}
	val := strings.TrimSpace(meta[idx+len(keyEq):])
	if strings.HasPrefix(val, `"`) {
		val = strings.TrimPrefix(val, `"`)
		if q := strings.Index(val, `"`); q >= 0 {
			return val[:q]
		}
	}
	return strings.Fields(val)[0]
}

// findBalancedXMLElement encontra o índice logo após o fechamento </name> correspondente.
func findBalancedXMLElement(b []byte, name string) (end int, ok bool) {
	openTag := "<" + name
	closeTag := "</" + name
	count := 0
	i := 0
	for i < len(b) {
		o := bytes.Index(b[i:], []byte(openTag))
		c := bytes.Index(b[i:], []byte(closeTag))
		if o == -1 && c == -1 {
			return 0, false
		}
		if o != -1 && (c == -1 || o < c) {
			// avançar até '>' desta abertura
			j := i + o
			k := bytes.IndexByte(b[j:], '>')
			if k == -1 {
				return 0, false
			}
			count++
			i = j + k + 1
			continue
		}
		if c != -1 {
			// avançar até '>' deste fechamento
			j := i + c
			k := bytes.IndexByte(b[j:], '>')
			if k == -1 {
				return 0, false
			}
			count--
			i = j + k + 1
			if count == 0 {
				return i, true
			}
			continue
		}
	}
	return 0, false
}

func nextTagEnd(b []byte) int {
	if len(b) == 0 || b[0] != '<' {
		return 0
	}
	i := bytes.IndexByte(b, '>')
	if i < 0 {
		return 0
	}
	return i + 1
}

func readInnerText(dec *xml.Decoder, start xml.StartElement) (string, error) {
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return sb.String(), err
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return sb.String(), nil
			}
		}
	}
}
