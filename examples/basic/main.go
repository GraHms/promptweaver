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
