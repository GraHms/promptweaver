package promptweaver

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func Test_Engine_BigComplexScenario_NoCodeBlockEvent(t *testing.T) {
	engine, sink := newDefaultEngine()

	input := strings.Join([]string{
		"Hello world.\nThis is a big paragraph of plain text.\n\n",
		"Some more text before tags.\n",
		"<CreateFile path=\"x/a.tsx\">console.log('hi')</CreateFile>\n",
		"<EditFile path=\"x/b.tsx\">edit content here</EditFile>\n",
		"Trailing plain text with multiple lines.\nLine 2.\nLine 3.\n",
		"<UnknownTag>should be audited</UnknownTag>\n",
		"Partial tag starts <CreateFile path=\"incomplete", // intentionally partial
	}, "")

	// Simula streaming usando chunkedReader
	chunks := []int{50, 30, 100, 10, 200}
	reader := newChunkedReader(input, chunks)

	err := engine.ProcessStream(reader, sink)
	require.NoError(t, err)

	// Inspecionar eventos
	for i, ev := range sink.events {
		t.Logf("Event %d: %#v\n", i, ev)
	}

	// Expectativas básicas
	assert.True(t, len(sink.events) > 5, "espera múltiplos eventos")
	assert.Equal(t, "PlainText", sink.events[0].(SectionEvent).Name)
	assert.Equal(t, "CreateFile", sink.events[1].(SectionEvent).Name)
	assert.Equal(t, "EditFile", sink.events[2].(SectionEvent).Name)
	assert.Equal(t, "PlainText", sink.events[len(sink.events)-1].(SectionEvent).Name)
}
