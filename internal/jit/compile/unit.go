package compile

import (
	"fmt"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
)

// Unit is what one compile reads: program data and module facts only, never
// the live heap (L11).
type Unit struct {
	Address  int
	Function *types.Function
	Module   transform.Module
	Tier     jit.Tier
	// Entry is the bytecode offset translation and lowering root at.
	Entry int
	// OSR marks u as rooted at a loop header instead of the function's own
	// entry: Entry alone cannot say this, since a header can sit at offset
	// 0 too.
	OSR bool
}

// Compile translates u at its Entry, optimizes it for its tier, and lowers
// it with m.
func Compile(u Unit, m Machine) (*jit.Code, error) {
	f, err := transform.Translate(u.Module, u.Address, u.Function, u.Entry)
	if err != nil {
		return nil, fmt.Errorf("compile: translate: %w", err)
	}
	if f == nil {
		return nil, fmt.Errorf("%w: translate", ErrUnsupported)
	}
	if err := ssa.Verify(f); err != nil {
		return nil, fmt.Errorf("compile: verify: %w", err)
	}

	stages := passes(u.Tier)
	if stages == nil {
		return nil, fmt.Errorf("%w: tier %d", ErrUnsupported, u.Tier)
	}
	pipeline := pass.NewPipeline[*ssa.Function]()
	for _, p := range stages {
		pipeline.Add(p)
	}
	if _, err := pipeline.Run(pass.NewManager(), f); err != nil {
		return nil, fmt.Errorf("compile: optimize: %w", err)
	}
	if err := ssa.Verify(f); err != nil {
		return nil, fmt.Errorf("compile: verify: %w", err)
	}

	code, exits, err := Lower(f, m, u.Function, u.Module.Objects, u.Address, u.OSR)
	if err != nil {
		return nil, err
	}
	c, err := jit.NewCode(u.Address, u.Entry, u.OSR, u.Tier, completion(f), code, exits)
	if err != nil {
		return nil, fmt.Errorf("compile: code: %w", err)
	}
	return c, nil
}

// completion is the value count a TrapReturn from f's own OpComplete
// leaves, 0 when f never completes (an ordinary function's RETURN count
// comes from its own Typ.Returns instead).
func completion(f *ssa.Function) int {
	for id := 0; id < f.Len(); id++ {
		if t := f.Block(id).Terminator; t.Op == ssa.OpComplete {
			return len(t.Args)
		}
	}
	return 0
}

// passes is the tier's pipeline: Baseline folds and eliminates dead code;
// Optimized runs the same order as O3.
func passes(tier jit.Tier) []pass.Pass[*ssa.Function] {
	switch tier {
	case jit.Baseline:
		return []pass.Pass[*ssa.Function]{transform.NewFoldPass(), transform.NewDCEPass()}
	case jit.Optimized:
		return []pass.Pass[*ssa.Function]{
			transform.NewFoldPass(), transform.NewPromotePass(), transform.NewForwardPass(),
			transform.NewCSEPass(), transform.NewGuardPass(), transform.NewHoistPass(), transform.NewDCEPass(),
		}
	default:
		return nil
	}
}
