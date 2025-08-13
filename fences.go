package promptweaver

import (
	"regexp"
	"strings"
)

// FencePlugin não é um Plugin XML; ele é embutido no Engine (emite CodeBlockEvent).
// Se quiser, poderíamos ter um adaptador para tratar CodeBlockEvent aqui.
// Incluí utilidades para metadados adicionais.

var fileKV = regexp.MustCompile(`file\s*=\s*"([^"]+)"`)

func ExtractFenceFile(meta string) string {
	m := fileKV.FindStringSubmatch(meta)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func ParseFenceHeader(header string) (lang, file string) {
	parts := strings.Fields(header)
	if len(parts) > 0 {
		lang = parts[0]
	}
	file = ExtractFenceFile(header)
	return
}
