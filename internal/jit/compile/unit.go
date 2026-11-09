package compile

import (
	"errors"
	"fmt"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/transform"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// Unit is what one compile reads: program data, module facts and a snapshot
// of function profiles only, never the live heap (L11).
type Unit struct {
	Address  int
	Function *types.Function
	Module   transform.Module
	// Profiles is the feedback snapshot the compile reads, for the root and
	// every function it joins.
	Profiles transform.Profiles
	Tier     jit.Tier
	// IP is the bytecode offset translation and lowering root at.
	IP int
	// OSR marks u as rooted at a loop header instead of the function's own
	// entry: IP alone cannot say this, since a header can sit at offset
	// 0 too.
	OSR bool
}

// Compile translates u at its IP, optimizes it for its tier, and lowers
// it with m. A lowering that finds an inlined operation it cannot give its
// own state (ErrInlined) is compiled again without inlining.
func Compile(u Unit, m Machine) (*jit.Code, error) {
	c, err := build(u, m, true)
	if errors.Is(err, ErrInlined) {
		return build(u, m, false)
	}
	return c, err
}

func build(u Unit, m Machine, inline bool) (*jit.Code, error) {
	f, members, err := transform.Translate(u.Module, u.Profiles, u.Address, u.Function, u.IP)
	if err != nil {
		return nil, fmt.Errorf("compile: translate: %w", err)
	}
	if f == nil {
		return nil, fmt.Errorf("%w: translate", ErrUnsupported)
	}
	if err := ssa.Verify(f); err != nil {
		return nil, fmt.Errorf("compile: verify: %w", err)
	}

	stages := passes(u, inline)
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

	assembly, err := Lower(f, m, u.Function, u.Module.Objects, u.Address, u.OSR, u.Tier == jit.Baseline && !u.OSR)
	if err != nil {
		return nil, err
	}
	var args []types.Kind
	if !u.OSR {
		args = arguments(u.Function)
	}
	c, err := jit.NewCode(
		u.Address, u.IP, u.OSR, u.Tier, completion(f),
		registers(u.Function), args, assembly,
	)
	if err != nil {
		return nil, fmt.Errorf("compile: code: %w", err)
	}
	for _, address := range members {
		c.Deps = append(c.Deps, jit.Dep{Address: address, Generation: u.Profiles.Of(address).Generation})
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

// passes is the tier's pipeline. Optimized also inlines leaf callees, when
// inline, and drops the bounds checks no refuted guard keeps (profiles
// Profile.Refuted).
func passes(u Unit, inline bool) []pass.Pass[*ssa.Function] {
	switch u.Tier {
	case jit.Baseline:
		return transform.O1()
	case jit.Optimized:
		stages := transform.O3(transform.NewBCEPass(u.Profiles))
		if inline {
			stages = append([]pass.Pass[*ssa.Function]{transform.NewInlinePass(u.Module)}, stages...)
		}
		return stages
	default:
		return nil
	}
}
