# Promptweaver

**Promptweaver** is a streaming, XML-lite **event parser** for model output.
You give it an `io.Reader`. You register a handful of tags.
It emits **events** the moment a tag closes.

No DOM. No schema. No nesting rules to memorize.
Just a clean, predictable way to turn text into actions.

---

## Explainer

Large models often speak in paragraphs but **act** in sections:

```xml
<think>Outline a plan…</think>
<create-file path="app/page.tsx">…code…</create-file>
<summary>What changed and why.</summary>
```

You don’t want to wait for the whole response. You want to **react as soon as a section is done**:

* print the plan,
* write the file,
* then show the summary.

Promptweaver reads the stream as it arrives and emits an event on each **closing tag**. Everything between `<open …>` and its matching `</close>` is delivered verbatim (for recognized tags). That keeps code intact—JSX, angle brackets, braces—without tripping a structured parser.

---

## What it does

* Reads bytes incrementally from an `io.Reader`.
* Recognizes **section tags** you register (with **aliases**).
* For each recognized tag:

    * Captures **all content** between `<tag …>` and the **first** matching `</tag>`.
    * Emits a `SectionEvent` with `Name`, `Attrs`, and `Content`.

That’s the contract.

---

## What it does **not** do

* It does **not** parse nested sections inside a recognized section.
  Once a section opens, **everything** until its closer is treated as plain text.
* It does **not** invent closes.
  If a section never closes, it runs to **EOF**; then Promptweaver emits what it has.
* It does **not** spawn goroutines or manage I/O for you.
  Your handlers are called synchronously.

---

## Installation

```bash
go get github.com/grahms/promptweaver
```


Go 1.21+ recommended.

---

## Quick Start

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yourorg/promptweaver"
)

func main() {
	// 1) Declare the tags your model will use
	reg := promptweaver.NewRegistry()
	reg.Register(promptweaver.SectionPlugin{Name: "think"})
	reg.Register(promptweaver.SectionPlugin{
		Name:    "write-file",
		Aliases: []string{"create-file", "dyad-write"}, // any spelling the model might use
	})
	reg.Register(promptweaver.SectionPlugin{Name: "summary"})

	// 2) Wire handlers for canonical names
	sink := promptweaver.NewHandlerSink()
	sink.RegisterHandler("think", func(ev promptweaver.SectionEvent) {
		fmt.Println("[THINK]\n" + strings.TrimSpace(ev.Content))
	})

	base := mustAbs("./workspace")
	sink.RegisterHandler("write-file", func(ev promptweaver.SectionEvent) {
		out := secureJoin(base, ev.Attrs["path"])
		_ = os.MkdirAll(filepath.Dir(out), 0o755)
		if err := os.WriteFile(out, []byte(ev.Content), 0o644); err != nil {
			fmt.Println("write error:", err)
			return
		}
		fmt.Printf("[CREATE FILE] %s ok\n", out)
	})

	sink.RegisterHandler("summary", func(ev promptweaver.SectionEvent) {
		fmt.Println("[SUMMARY]\n" + strings.TrimSpace(ev.Content))
	})

	// 3) Stream
	engine := promptweaver.NewEngine(reg)
	src := promptweaver.ReaderFromString(
		`<think>plan</think>` +
			`<create-file path="main.ts">console.log("hi")</create-file>` +
			`<summary>done</summary>`,
	)

	if err := engine.ProcessStream(src, sink); err != nil {
		panic(err)
	}
}

func mustAbs(p string) string { a, _ := filepath.Abs(p); return a }

// deny writes outside base
func secureJoin(base, rel string) string {
	base = filepath.Clean(base)
	path := filepath.Clean(filepath.Join(base, rel))
	if path != base && !strings.HasPrefix(path, base+string(os.PathSeparator)) {
		panic("refusing to write outside base: " + rel)
	}
	return path
}
```

---

## API

```go
type SectionPlugin struct {
	Name    string   // canonical section name (what handlers see)
	Aliases []string // alternative spellings your model might use
}

type SectionEvent struct {
	Name    string            // canonical name
	Attrs   map[string]string // attribute keys are lowercased
	Content string            // everything between <open> and </close>
}

// Registry maps aliases -> canonical
reg := promptweaver.NewRegistry()
reg.Register(promptweaver.SectionPlugin{Name: "think"})
reg.Register(promptweaver.SectionPlugin{Name: "write-file", Aliases: []string{"create-file"}})

// Handlers route by canonical name
sink := promptweaver.NewHandlerSink()
sink.RegisterHandler("write-file", func(ev promptweaver.SectionEvent) { /* ... */ })

engine := promptweaver.NewEngine(reg)
_ = engine.ProcessStream(reader, sink)
```

---

## Streaming Semantics

* **Emit on close**: an event fires as soon as `</tag>` is read. No need to buffer the whole response.
* **Flat model**: inside a recognized section, Promptweaver **does not** parse inner tags; it treats them as content. This is why code survives intact.
* **Unknown tags**:

    * outside any recognized section: ignored.
    * inside a recognized section: treated as literal text.
* **EOF**: if the stream ends with a recognized section still open, that section is emitted with whatever content arrived.

---

## Tag Grammar

Promptweaver accepts a small, well-defined subset:

* **Open**

  ```
  <name a="x" b='y' c={expr}>
  ```

    * `name`: letters, digits, `_`, `-`; case-insensitive.
    * Attributes:

        * keys are lowercased.
        * values can be:

            * `"double-quoted"`
            * `'single-quoted'`
            * `{ … }` (JSX-style). Braces are balanced; quotes inside are skipped.

* **Close**

  ```
  </name>
  </   name   >
  ```

    * Spaces after `</` and before `>` are allowed.
    * Name matching is **case-insensitive**.

* **Self-closing**

  ```
  <name …/>
  ```

    * If `name` is recognized, an event is emitted with empty content.

* **Aliases**

    * Open with `<create-file>` and close with `</dyad-write>` if both alias to the same canonical (e.g., `write-file`).
    * If a closer name isn’t in the alias map, Promptweaver falls back to a **literal** match with the original open name.

---

## Practical Recipes

### Multi-file code generation

Model output:

```xml
<think>Outline</think>
<create-file path="app/page.tsx">…</create-file>
<create-file path="lib/api.ts">…</create-file>
<summary>Notes and next steps.</summary>
```

Handlers:

* `think` → print to console.
* `write-file` → write to a sandboxed workspace; run a linter per file if you like.
* `summary` → display as a final report.

### Tool calls without JSON

```xml
<think>Plan</think>
<run-bash cwd="." timeout="30s">npm test</run-bash>
<summary>What failed and why.</summary>
```

Register `run-bash` and gate the handler with your policies. Since it is a recognized tag, the command body is captured exactly as written.

### Incremental extraction

```xml
<record id="1">…</record>
<record id="2">…</record>
<record id="3">…</record>
```

Each record arrives as soon as it closes. You can ingest them one by one.

---

## Debugging

* **See the exact text the model sent**

  ```go
  tee := io.TeeReader(reader, os.Stdout)
  _ = engine.ProcessStream(tee, sink)
  ```

* **Test with small chunks**

  ```go
  type chunkedReader struct{ data []byte; pos, chunk int }
  func (c *chunkedReader) Read(p []byte) (int, error) {
  	if c.pos >= len(c.data) { return 0, io.EOF }
  	n := c.chunk
  	if n > len(c.data)-c.pos { n = len(c.data)-c.pos }
  	copy(p, c.data[c.pos:c.pos+n])
  	c.pos += n
  	return n, nil
  }
  ```

Feed the engine with `chunk=32` to exercise the tokenizer.

---

## Security Notes

* Treat attributes as untrusted input. If you write files, **sanitize paths** and fence them under a base directory (see `secureJoin` in the Quick Start).
* Apply allow-lists in handlers (`path` prefixes, URL hosts, command names) as needed by your environment.

---

## FAQ

**Why not JSON?**
Because code and prose contain braces and commas. JSON is brittle under truncation and edits. Tags degrade more gracefully and are easier to repair mentally.

**Can I nest sections?**
No. That is a deliberate constraint. Inside a recognized section, everything is text until the matching close. If you need true nesting, build a different layer on top.

**What happens if the model never closes a tag?**
The section emits at EOF with whatever content arrived. Fix the prompt to include the closer.

**Do attribute keys keep their case?**
They’re lowercased in the event. Values are returned without quotes (and with braces preserved for `{…}`).

**Can a closer include spaces?**
Yes: `</   create-file   >` is accepted.

---

## Design Notes

* **Flat, one-section state.** This keeps the rules simple and the behavior predictable. Code inside sections won’t collide with the parser.
* **Alias mapping.** The model can vary tag names; your handlers see only canonical names.
* **Tokenizer tolerance.** Whitespace, quoted strings, and JSX braces are handled in a way that follows the stream without guessing.

Short code. Clear rules. Immediate effects.

