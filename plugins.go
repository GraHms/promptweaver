package promptweaver

import (
	"encoding/xml"
	"strings"
)

type SectionPlugin struct {
	Name string
}

func (p SectionPlugin) Names() []string { return []string{p.Name} }

func (p SectionPlugin) HandleXMLElement(raw string, dec *xml.Decoder, start xml.StartElement, sink EventSink) error {
	// Extract attributes into a map
	attrs := make(map[string]string, len(start.Attr))
	for _, attr := range start.Attr {
		attrs[attr.Name.Local] = attr.Value
	}

	// Get inner text
	body, err := readInnerText(dec, start)
	if err != nil && err.Error() != "EOF" {
		return err
	}

	// Fire event with attributes included
	sink.OnEvent(SectionEvent{
		Name:     start.Name.Local,
		Content:  strings.TrimSpace(body),
		Metadata: attrs,
	})

	return nil
}
