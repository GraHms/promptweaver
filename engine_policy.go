package promptweaver

type UnknownTagPolicy int

const (
	UnknownDrop    UnknownTagPolicy = iota // strict: ignorar
	UnknownAudit                           // emitir SectionEvent só p/ observabilidade
	UnknownLenient                         // igual ao acima; executor pode usar
)

type Engine struct {
	reg    *Registry
	policy UnknownTagPolicy
}
