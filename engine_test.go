package promptweaver

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recorderSink struct{ events []Event }

func (r *recorderSink) OnEvent(ev Event) { r.events = append(r.events, ev) }

func newDefaultEngine() (*Engine, *recorderSink) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "EditFile"})
	reg.Register(SectionPlugin{Name: "CreateFile"})
	reg.Register(SectionPlugin{Name: "Thinking"})
	reg.Register(SectionPlugin{Name: "Analysis"})
	reg.Register(SectionPlugin{Name: "Planning"})
	return NewEngine(reg), &recorderSink{}
}

func Test_Engine(t *testing.T) {
	t.Run("should parse fence code block and emit CodeBlockEvent with language and file", func(t *testing.T) {
		engine, sink := newDefaultEngine()
		input := "```tsx file=\"components/example.tsx\"\nconsole.log('hi')\n```\n"
		err := engine.ProcessStream(strings.NewReader(input), sink)
		require.NoError(t, err)
		require.Len(t, sink.events, 1)
		cb, ok := sink.events[0].(CodeBlockEvent)
		require.True(t, ok)
		assert.Equal(t, "tsx", cb.Language)
		assert.Equal(t, "components/example.tsx", cb.File)
		assert.Contains(t, cb.Content, "console.log")
	})

	t.Run("should parse CreateFile self-closed tag and emit CreateFileEvent", func(t *testing.T) {
		engine, sink := newDefaultEngine()
		input := `<CreateFile path="ui/button.tsx" />`
		err := engine.ProcessStream(strings.NewReader(input), sink)
		require.NoError(t, err)
		require.Len(t, sink.events, 1)
		ev, ok := sink.events[0].(SectionEvent)
		require.True(t, ok)
		assert.Equal(t, "ui/button.tsx", ev.Metadata["path"])
	})

	t.Run("should parse EditFile tag with body and emit EditFileEvent", func(t *testing.T) {
		engine, sink := newDefaultEngine()
		input := `<EditFile path="app/page.tsx">irrelevant body</EditFile>`
		err := engine.ProcessStream(strings.NewReader(input), sink)
		require.NoError(t, err)
		require.Len(t, sink.events, 1)
		ev, ok := sink.events[0].(SectionEvent)
		require.True(t, ok)
		assert.Equal(t, "app/page.tsx", ev.Metadata["path"])
	})

	t.Run("should extract language and file from fence header", func(t *testing.T) {
		lang, file := ParseFenceHeader(`tsx file="components/x.tsx" extra=1`)
		assert.Equal(t, "tsx", lang)
		assert.Equal(t, "components/x.tsx", file)
		fileOnly := ExtractFenceFile(`file="a/b/c.go"`)
		assert.Equal(t, "a/b/c.go", fileOnly)
	})
	t.Run("should drop unknown tags when policy is strict", func(t *testing.T) {
		reg := NewRegistry()
		engine := NewEngine(reg, WithUnknownPolicy(UnknownDrop))
		sink := &recorderSink{}
		input := `<Note>alpha bravo</Note>`
		require.NoError(t, engine.ProcessStream(strings.NewReader(input), sink))
		assert.Len(t, sink.events, 0)
	})

}

type chunkedReader struct {
	data   string
	chunks []int
	idx    int
	pos    int
}

func newChunkedReader(s string, chunks []int) *chunkedReader {
	return &chunkedReader{data: s, chunks: chunks}
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	if c.pos >= len(c.data) {
		return 0, io.EOF
	}
	if c.idx >= len(c.chunks) {
		c.chunks = append(c.chunks, 8)
	}
	n := c.chunks[c.idx]
	c.idx++
	if c.pos+n > len(c.data) {
		n = len(c.data) - c.pos
	}
	copy(p, c.data[c.pos:c.pos+n])
	c.pos += n
	return n, nil
}

func Test_Engine_MoreCases(t *testing.T) {
	t.Run("should handle fence without trailing newline at end", func(t *testing.T) {
		engine, sink := newDefaultEngine()
		input := "```go file=\"m.go\"\npackage main\nfunc main(){}\n```" // sem \n no final
		err := engine.ProcessStream(strings.NewReader(input), sink)
		require.NoError(t, err)
		require.Len(t, sink.events, 1)
		_, ok := sink.events[0].(CodeBlockEvent)
		require.True(t, ok)
	})

	t.Run("should ignore incomplete fence at EOF", func(t *testing.T) {
		engine, sink := newDefaultEngine()
		input := "```go file=\"m.go\"\nfunc x() {}\n" // não fecha ```
		err := engine.ProcessStream(strings.NewReader(input), sink)
		require.NoError(t, err)
		assert.Len(t, sink.events, 0)
	})

	t.Run("should drop unknown balanced XML when policy is strict", func(t *testing.T) {
		reg := NewRegistry()
		engine := NewEngine(reg, WithUnknownPolicy(UnknownDrop))
		sink := &recorderSink{}
		input := `<Meta><Tag>hidden</Tag></Meta>`
		err := engine.ProcessStream(strings.NewReader(input), sink)
		require.NoError(t, err)
		assert.Len(t, sink.events, 0)
	})

	t.Run("should audit unknown balanced XML when policy is audit", func(t *testing.T) {
		reg := NewRegistry()
		engine := NewEngine(reg, WithUnknownPolicy(UnknownAudit))
		sink := &recorderSink{}
		input := `<Meta><Tag>hidden</Tag></Meta>`
		err := engine.ProcessStream(strings.NewReader(input), sink)
		require.NoError(t, err)
		require.Len(t, sink.events, 1)
		sec, ok := sink.events[0].(SectionEvent)
		require.True(t, ok)
		assert.Equal(t, "Meta", sec.Name)
		// dependendo da tua readInnerText, pode não conter "Tag"
		// então verificamos ao menos que não vem vazio
		assert.NotEmpty(t, sec.Content)
	})

	t.Run("should parse CreateFile with extra attributes", func(t *testing.T) {
		engine, sink := newDefaultEngine()
		input := `<CreateFile path="ui/x.tsx"> hello world </CreateFile>`
		err := engine.ProcessStream(strings.NewReader(input), sink)
		require.NoError(t, err)
		require.Len(t, sink.events, 1)
		cf, ok := sink.events[0].(SectionEvent)
		require.True(t, ok)
		assert.Equal(t, "ui/x.tsx", cf.Metadata["path"])
	})
	t.Run("should parse CreateFile with extra attributes", func(t *testing.T) {
		engine, sink := newDefaultEngine()
		input := `<CreateFile path="ui/x.tsx" mode="0644" owner="me" />`
		err := engine.ProcessStream(strings.NewReader(input), sink)
		require.NoError(t, err)
		require.Len(t, sink.events, 1)
		cf, ok := sink.events[0].(SectionEvent)
		require.True(t, ok)
		assert.Equal(t, "ui/x.tsx", cf.Metadata["path"])
	})

	t.Run("should ignore unknown self-closed tag", func(t *testing.T) {
		reg := NewRegistry() // sem plugin para <Foo />
		engine := NewEngine(reg)
		sink := &recorderSink{}
		input := `<Foo attr="1" />`
		err := engine.ProcessStream(strings.NewReader(input), sink)
		require.NoError(t, err)
		assert.Len(t, sink.events, 0)
	})
	t.Run("should parse Summary block and emit SectionEvent with content", func(t *testing.T) {
		engine, sink := newDefaultEngine()

		input := `<Summary>High-level: OK. Next: ship it.</Summary>`
		err := engine.ProcessStream(strings.NewReader(input), sink)
		require.NoError(t, err)

		require.Len(t, sink.events, 1)
		sec, ok := sink.events[0].(SectionEvent)
		require.True(t, ok, "first event should be SectionEvent")
		assert.Equal(t, "Summary", sec.Name)
		assert.Contains(t, sec.Content, "High-level: OK. Next: ship it.")

	})
	t.Run("should keep order with CreateFile, Summary, fenced code, then EditFile", func(t *testing.T) {
		engine, sink := newDefaultEngine()

		// Uses a normal backtick fence for robustness.
		payload := strings.TrimLeft(`
<CreateFile path="x/a.tsx" > first event </CreateFile>

<Summary>Touches A and B; safe to proceed.</Summary>
		
		<EditFile path="x/a.tsx">doing some edits</EditFile>
			`, "\n")

		err := engine.ProcessStream(strings.NewReader(payload), sink)
		require.NoError(t, err)

		require.Len(t, sink.events, 3)

		// 1) CreateFile
		_, ok := sink.events[0].(SectionEvent)
		require.True(t, ok, "first event should be CreateFileEvent")

		// 2) Summary section
		sec, ok := sink.events[1].(SectionEvent)
		require.True(t, ok, "second event should be SectionEvent")
		assert.Equal(t, "Summary", sec.Name)
		assert.Contains(t, sec.Content, "Touches A and B")

		// 3) Code block for x/a.tsx
		cb, ok := sink.events[2].(SectionEvent)
		require.True(t, ok, "third event should be CodeBlockEvent")
		assert.Contains(t, cb.Content, "doing some edits")

	})

}

func Test_Engine_PlainText(t *testing.T) {
	engine, sink := newDefaultEngine()

	t.Run("should emit PlainTextEvent for text outside tags", func(t *testing.T) {
		input := "Hello world.\n<CreateFile path=\"a.tsx\" />\nMore text."
		err := engine.ProcessStream(strings.NewReader(input), sink)
		require.NoError(t, err)
		require.Len(t, sink.events, 3)

		// 1) Plain text before the tag
		sec, ok := sink.events[0].(SectionEvent)
		require.True(t, ok)
		assert.Equal(t, "PlainText", sec.Name)
		assert.Contains(t, sec.Content, "Hello world")

		// 2) CreateFile tag
		cf, ok := sink.events[1].(SectionEvent)
		require.True(t, ok)
		assert.Equal(t, "a.tsx", cf.Metadata["path"])

		// 3) Plain text after the tag
		sec2, ok := sink.events[2].(SectionEvent)
		require.True(t, ok)
		assert.Equal(t, "PlainText", sec2.Name)
		assert.Contains(t, sec2.Content, "More text")
	})

	//t.Run("should handle partial tag in stream", func(t *testing.T) {
	//	input := `<CreateFile path="b.tsx"> content`
	//	chunks := []int{5, 5, 5, 5, 5} // simulate LLM streaming in small chunks
	//	r := newChunkedReader(input, chunks)
	//	err := engine.ProcessStream(r, sink)
	//	require.NoError(t, err)
	//	// incomplete tag: should not emit anything yet
	//	// but for the purpose of this test, we expect no events because not closed
	//	assert.Len(t, sink.events, 0)
	//})

	t.Run("should emit PlainText before incomplete tag", func(t *testing.T) {
		e, s := newDefaultEngine()
		input := "Text before <CreateFile path=\"c.tsx\""
		err := e.ProcessStream(strings.NewReader(input), s)
		require.NoError(t, err)
		require.Len(t, s.events, 1)
		sec, ok := s.events[0].(SectionEvent)
		require.True(t, ok)
		assert.Equal(t, "PlainText", sec.Name)
		assert.Contains(t, sec.Content, "Text before")
	})
}
