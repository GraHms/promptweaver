package promptweaver

import (
	"io"
	"strings"
	"testing"
)

// chunkedReader para simular streaming em testes.
type chunkedReader struct {
	data  []byte
	pos   int
	chunk int
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	if c.pos >= len(c.data) {
		return 0, io.EOF
	}
	n := c.chunk
	if n > len(c.data)-c.pos {
		n = len(c.data) - c.pos
	}
	copy(p, c.data[c.pos:c.pos+n])
	c.pos += n
	return n, nil
}

func newSinkCatcher(tags ...string) (*HandlerSink, *[]SectionEvent) {
	var out []SectionEvent
	s := NewHandlerSink()
	for _, tag := range tags {
		name := tag
		s.RegisterHandler(name, func(ev SectionEvent) {
			out = append(out, ev)
		})
	}
	return s, &out
}

func Test_Engine_Should_Emit_On_Registered_Close(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "think"})
	reg.Register(SectionPlugin{Name: "summary"})
	sink, got := newSinkCatcher("think", "summary")

	en := NewEngine(reg)
	input := `<think a="x">hello</think><summary>done</summary>`
	if err := en.ProcessStream(ReaderFromString(input), sink); err != nil {
		t.Fatalf("ProcessStream error: %v", err)
	}
	if len(*got) != 2 {
		t.Fatalf("want 2 events, got %d", len(*got))
	}
	if (*got)[0].Name != "think" || (*got)[0].Attrs["a"] != "x" || (*got)[0].Content != "hello" {
		t.Fatalf("unexpected think event: %+v", (*got)[0])
	}
	if (*got)[1].Name != "summary" || (*got)[1].Content != "done" {
		t.Fatalf("unexpected summary event: %+v", (*got)[1])
	}
}

func Test_Engine_Should_Passthrough_Unknown_SelfClosing_To_Parent(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "think"})
	sink, got := newSinkCatcher("think")

	en := NewEngine(reg)
	input := `<think>before<img src="x"/>
end</think>`
	if err := en.ProcessStream(ReaderFromString(input), sink); err != nil {
		t.Fatalf("ProcessStream error: %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("want 1 event, got %d", len(*got))
	}
	if (*got)[0].Name != "think" {
		t.Fatalf("unexpected name: %s", (*got)[0].Name)
	}
	if !strings.Contains((*got)[0].Content, `<img src="x"/>`) {
		t.Fatalf("parent content missing passthrough img: %q", (*got)[0].Content)
	}
}

func Test_Engine_Should_Ignore_Unmatched_Closing_Tag_Gracefully(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "think"})
	sink, got := newSinkCatcher("think")

	en := NewEngine(reg)
	input := `</bogus><think>hi</think></bogus>`
	if err := en.ProcessStream(ReaderFromString(input), sink); err != nil {
		t.Fatalf("ProcessStream error: %v", err)
	}
	if len(*got) != 1 || (*got)[0].Name != "think" || (*got)[0].Content != "hi" {
		t.Fatalf("unexpected events: %+v", *got)
	}
}

func Test_Engine_Should_Emit_On_EOF_Unclosed(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "summary"})
	sink, got := newSinkCatcher("summary")

	en := NewEngine(reg)
	input := `<summary>partial`
	if err := en.ProcessStream(ReaderFromString(input), sink); err != nil {
		t.Fatalf("ProcessStream error: %v", err)
	}
	if len(*got) != 1 || (*got)[0].Name != "summary" || (*got)[0].Content != "partial" {
		t.Fatalf("unexpected events: %+v", *got)
	}
}

func Test_Engine_Should_Stream_Partial_Tags_In_Chunks(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "think"})
	reg.Register(SectionPlugin{Name: "summary"})
	sink, got := newSinkCatcher("think", "summary")

	en := NewEngine(reg)
	input := `<think>Hello</think><summary>Done</summary>`
	reader := &chunkedReader{data: []byte(input), chunk: 3}
	if err := en.ProcessStream(reader, sink); err != nil {
		t.Fatalf("ProcessStream error: %v", err)
	}
	if len(*got) != 2 {
		t.Fatalf("want 2 events, got %d", len(*got))
	}
	if (*got)[0].Content != "Hello" || (*got)[1].Content != "Done" {
		t.Fatalf("unexpected contents: %#v", *got)
	}
}

func Test_Engine_Should_Parse_Attrs_Single_And_Double_Quotes(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "think"})
	sink, got := newSinkCatcher("think")

	en := NewEngine(reg)
	input := `<think a='1' b="two">x</think>`
	if err := en.ProcessStream(ReaderFromString(input), sink); err != nil {
		t.Fatalf("ProcessStream error: %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("want 1 event, got %d", len(*got))
	}
	ev := (*got)[0]
	if ev.Attrs["a"] != "1" || ev.Attrs["b"] != "two" || ev.Content != "x" {
		t.Fatalf("unexpected event: %+v", ev)
	}
}

func Test_Engine_Should_Match_Aliases_OpenClose(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "write-file", Aliases: []string{"dyad-write", "create-file"}})
	sink, got := newSinkCatcher("write-file")

	en := NewEngine(reg)
	input := `<dyad-write path="todo.tsx">CODE</create-file>`
	if err := en.ProcessStream(ReaderFromString(input), sink); err != nil {
		t.Fatalf("ProcessStream error: %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("want 1 event, got %d", len(*got))
	}
	ev := (*got)[0]
	if ev.Name != "write-file" {
		t.Fatalf("expected canonical name 'write-file', got %q", ev.Name)
	}
	if ev.Attrs["path"] != "todo.tsx" {
		t.Fatalf("path attr mismatch: %+v", ev.Attrs)
	}
	if ev.Content != "CODE" {
		t.Fatalf("unexpected content: %q", ev.Content)
	}
	if strings.Contains(ev.Content, "</create-file>") {
		t.Fatalf("content leaked closing tag: %q", ev.Content)
	}
}

func Test_Engine_SelfClosing_Recognized_Emits(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "summary"})
	sink, got := newSinkCatcher("summary")

	en := NewEngine(reg)
	if err := en.ProcessStream(ReaderFromString(`<summary/>`), sink); err != nil {
		t.Fatalf("ProcessStream error: %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("want 1 event, got %d", len(*got))
	}
	if (*got)[0].Name != "summary" || (*got)[0].Content != "" {
		t.Fatalf("unexpected: %+v", (*got)[0])
	}
}

func Test_Engine_TextOutsideTags_Ignored(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "think"})
	sink, got := newSinkCatcher("think")

	en := NewEngine(reg)
	input := `leading text<think>x</think>trailing text`
	if err := en.ProcessStream(ReaderFromString(input), sink); err != nil {
		t.Fatalf("ProcessStream error: %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("want 1 event, got %d", len(*got))
	}
	if (*got)[0].Content != "x" {
		t.Fatalf("unexpected think content: %q", (*got)[0].Content)
	}
}

func Test_Engine_AutoClose_OnEOF_With_Alias(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "write-file", Aliases: []string{"dyad-write"}})
	sink, got := newSinkCatcher("write-file")

	en := NewEngine(reg)
	input := `<dyad-write path="x.txt">partial`
	if err := en.ProcessStream(ReaderFromString(input), sink); err != nil {
		t.Fatalf("ProcessStream error: %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("want 1 event, got %d", len(*got))
	}
	if (*got)[0].Name != "write-file" || (*got)[0].Attrs["path"] != "x.txt" || (*got)[0].Content != "partial" {
		t.Fatalf("unexpected event: %+v", (*got)[0])
	}
}
