
# promptweaver

`promptweaver` is a Go library for **parsing, structuring, and streaming hybrid text** — a mix of **plain text**, **XML-like tags**, and **metadata** — in a **progressive, event-driven way**.

It’s perfect for:

* Parsing **LLM outputs** or agent instructions.
* Handling **mixed content**: structured tags and free text.
* Incremental processing of **streamed text**, without waiting for the full input.
* Extracting meaningful events from text for automated workflows or analytics.

---

### Installation

```bash
go get github.com/grahms/promptweaver
```

---

## Why promptweaver

Traditional XML parsers fail when:

* Tags are **dynamic or unknown**, e.g., `<Thinking>` or `<CreateFile>`.
* Text is **partial or streaming**, e.g., LLM output arriving in chunks.
* Mixed content combines **plain text with tags**.

`promptweaver` solves this by:

* Emitting **SectionEvent** for any meaningful content.
* **Incremental parsing**: handles incomplete tags safely.
* Preserving **plain text context** outside tags.
* Configurable **unknown tag handling** (`audit` or `drop`).

---

## Core Concepts

* **Engine**: Reads a stream of text and emits `SectionEvent`s.
* **SectionEvent**: Represents any chunk of meaningful content (tag or plain text).
  Fields: `Name string`, `Content string`, `Metadata map[string]string`
* **EventSink**: Receives events from the engine.
* **Plugin**: Custom handler for tags.
* **UnknownTagPolicy**: Determines what happens to unknown tags.

---

## Examples

### 1. Parsing Instructions with SectionEvent

```go
reg := promptweaver.NewRegistry()
reg.Register(promptweaver.SectionPlugin{name: "CreateFile"})
reg.Register(promptweaver.SectionPlugin{name: "EditFile"})
reg.Register(promptweaver.SectionPlugin{name: "Summary"})

engine := promptweaver.NewEngine(reg)
sink := &recorderSink{}

input := `
Hello team!
<CreateFile path="main.go">package main</CreateFile>
Keep building.
<EditFile path="main.go">func RunAgent() {}</EditFile>
<Summary>Agent created and edited</Summary>
`

engine.ProcessStream(strings.NewReader(input), sink)

for _, ev := range sink.events {
    fmt.Printf("[%s] %s\n", ev.Name, ev.Content)
}
```

**Output:**

```
[PlainText] Hello team!
[CreateFile] package main
[PlainText] Keep building.
[EditFile] func RunAgent() {}
[Summary] Agent created and edited
```

---

### 2. Handling Plain Text

Plain text outside tags is captured automatically:

```xml
Hello world!
<CreateFile path="app.go">package main</CreateFile>
More text here.
```

**Emitted Events:**

```
[PlainText] Hello world!
[CreateFile] package main
[PlainText] More text here.
```

---

### 3. Streaming & Partial Tags

If a tag is split across chunks (streaming input):

```go
chunks := []string{
    "Some text <Create",
    "File path=\"x.go\">package",
    " main</CreateFile> More text",
}
reader := strings.NewReader(strings.Join(chunks, ""))
engine.ProcessStream(reader, sink)
```

**Output preserves order and completeness:**

```
[PlainText] Some text
[CreateFile] package main
[PlainText] More text
```

---

### 4. Unknown Tags with Audit

```go
engine := promptweaver.NewEngine(reg, promptweaver.WithUnknownPolicy(promptweaver.UnknownAudit))
input := `<Note>This is a note</Note>`
engine.ProcessStream(strings.NewReader(input), sink)
```

**Emitted SectionEvent:**

```
[Note] This is a note
```

---

### 5. Nested Tags

```xml
<Summary>
  Important:
  <EditFile path="x.go">func main() {}</EditFile>
</Summary>
```

**Events:**

```
[Summary] Important:
[EditFile] func main() {}
```

---

## Real-World Scenarios

* **Vibe Coding**: Track file modifications and instructions in real-time.
* **Agent Workflows**: Parse multi-step LLM instructions incrementally.
* **CLI Capture**: Record commands and outputs with metadata.
* **Streaming Logs**: Extract actionable events while preserving context.
* **Progressive File Generation**: Generate files directly from instructions.

---

This version keeps everything **uniform under `SectionEvent`**, which simplifies event handling and makes it easy to register plugins for any tag while still preserving plain text.

---

Perfect — here’s a **full example project** using only `SectionEvent`, showing a realistic, streaming, multi-tag scenario like you’d see in a Vibe coding session or agent-driven instructions.

---

# Full Example Project: “VibeFlow”

```go
package main

import (
	"fmt"
	"strings"
	"github.com/grahms/promptweaver"
)

// recorderSink collects all SectionEvents emitted by the engine
type recorderSink struct {
	events []promptweaver.Event
}

func (r *recorderSink) OnEvent(ev promptweaver.Event) {
	r.events = append(r.events, ev)
}

func main() {
	// Step 1: Create a registry and register known tags
	reg := promptweaver.NewRegistry()
	reg.Register(promptweaver.SectionPlugin{Name: "CreateFile"})
	reg.Register(promptweaver.SectionPlugin{Name: "EditFile"})
	reg.Register(promptweaver.SectionPlugin{Name: "Summary"})
	reg.Register(promptweaver.SectionPlugin{Name: "Thinking"})
	reg.Register(promptweaver.SectionPlugin{Name: "Analysis"})

	// Step 2: Create the engine
	engine := promptweaver.NewEngine(reg, promptweaver.WithUnknownPolicy(promptweaver.UnknownAudit))
	sink := &recorderSink{}

	// Step 3: Input text simulating streaming instructions from an LLM or agent
	input := strings.TrimSpace(`
Hello team!

<Thinking>What files do we need?</Thinking>

<CreateFile path="agent.go">
package main

func StartAgent() {}
</CreateFile>

More context for our workflow.

<EditFile path="agent.go">
func StartAgent() {
    fmt.Println("Agent started")
}
</EditFile>

<Summary>Agent file created and updated.</Summary>

Final note: keep monitoring logs.
`)

	// Step 4: Process the text
	engine.ProcessStream(strings.NewReader(input), sink)

	// Step 5: Print all SectionEvents
	for _, ev := range sink.events {
		fmt.Printf("[%s] %s\n", ev.(promptweaver.SectionEvent).Name, strings.TrimSpace(ev.(promptweaver.SectionEvent).Content))
		if len(ev.(promptweaver.SectionEvent).Metadata) > 0 {
			fmt.Printf("  Metadata: %+v\n", ev.(promptweaver.SectionEvent).Metadata)
		}
	}
}

```

---

## Expected Output

```
[PlainText] Hello team!
[Thinking] What files do we need?
[CreateFile] package main

func StartAgent() {}
  Metadata: map[path:agent.go]
[PlainText] More context for our workflow.
[EditFile] func StartAgent() {
    fmt.Println("Agent started")
}
  Metadata: map[path:agent.go]
[Summary] Agent file created and updated.
[PlainText] Final note: keep monitoring logs.
```

---

## Key Points in This Example

1. **All content is captured via `SectionEvent`**, whether it’s plain text or structured tags.
2. **Metadata is preserved** automatically for registered tags, e.g., `path="agent.go"`.
3. **Streaming-safe**: Even if the text were read in chunks, the engine would still handle partial tags correctly.
4. **Unknown tags**: Any unregistered tags would be audited (`UnknownAudit`) or dropped (`UnknownDrop`) depending on your policy.
5. **Nested or multi-line content**: Works perfectly with tags containing multiple lines or internal indentation.

---

This full example demonstrates how `promptweaver` can be used to **incrementally parse instructions, manage files, and capture plain text context** in a unified event-driven approach.

---
