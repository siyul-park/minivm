package interp

import "github.com/siyul-park/minivm/types"

type planner interface {
	plan(*compileInput) ([]plan, error)
}

type compileInput struct {
	interpreter *Interpreter
	address     int
	function    *types.Function
	globals     []types.Kind
	functions   map[int]*types.Function
	installed   bool
}

type plan struct {
	entry  entry
	blocks []block
	spill  spillPolicy
}

type entry struct {
	anchor anchor
	kind   entryKind
}

type entryKind uint8

const (
	entryFunction entryKind = iota
	entryLoop
	entryModule
)

type block struct {
	offset int
	state  state
	steps  []step
	term   terminator
}

type state struct {
	slots []slot
}

type slot struct {
	kind        types.Kind
	ref         int
	refKnown    bool
	callee      int
	calleeKnown bool
}

type terminator struct {
	kind    terminatorKind
	ip      int
	targets []int
}

type terminatorKind uint8

const (
	terminateFallthrough terminatorKind = iota
	terminateBranch
	terminateBranchIf
	terminateBranchTable
	terminateReturn
	terminateComplete
	terminateFallback
)

type spillPolicy uint8

const (
	spillAllowed spillPolicy = iota
	spillForbidden
)

func newCompileInput(i *Interpreter, addr int) (*compileInput, bool) {
	fn, ok := i.function(addr)
	if !ok || fn == nil || len(fn.Code) == 0 {
		return nil, false
	}
	globals := make([]types.Kind, len(i.globals))
	for idx, global := range i.globals {
		globals[idx] = global.Kind()
	}
	functions := make(map[int]*types.Function)
	for fnAddr := range i.instrs {
		if target, ok := i.function(fnAddr); ok {
			functions[fnAddr] = target
		}
	}
	return &compileInput{
		interpreter: i,
		address:     addr,
		function:    fn,
		globals:     globals,
		functions:   functions,
		installed:   i.stub(addr) != nil,
	}, true
}

func (p plan) valid() bool {
	if len(p.blocks) == 0 {
		return false
	}
	switch p.entry.kind {
	case entryFunction:
		if p.entry.anchor.addr <= 0 || p.entry.anchor.ip != 0 {
			return false
		}
	case entryLoop:
		if p.entry.anchor.addr <= 0 || p.entry.anchor.ip <= 0 {
			return false
		}
	case entryModule:
		if p.entry.anchor.addr != 0 || p.entry.anchor.ip != 0 {
			return false
		}
	default:
		return false
	}
	offsets := make(map[int]struct{}, len(p.blocks))
	for _, block := range p.blocks {
		if _, ok := offsets[block.offset]; ok {
			return false
		}
		offsets[block.offset] = struct{}{}
	}
	if _, ok := offsets[p.entry.anchor.ip]; !ok {
		return false
	}
	for _, block := range p.blocks {
		for _, target := range block.term.targets {
			if _, ok := offsets[target]; !ok {
				return false
			}
		}
	}
	return true
}
