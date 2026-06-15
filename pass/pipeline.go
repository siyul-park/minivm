package pass

// Pipeline runs an ordered sequence of transforms over an IR unit of type U,
// invalidating stale analyses between passes. It mirrors LLVM's PassManager.
type Pipeline[U any] struct {
	passes []Pass[U]
}

func NewPipeline[U any]() *Pipeline[U] {
	return &Pipeline[U]{}
}

// AddPass appends a transform to the pipeline.
func (p *Pipeline[U]) AddPass(pass Pass[U]) {
	p.passes = append(p.passes, pass)
}

// Run executes each transform in order and returns the transformed unit.
func (p *Pipeline[U]) Run(m *Manager, unit U) (U, error) {
	for _, pass := range p.passes {
		preserved, err := pass.Run(m, unit)
		if err != nil {
			return unit, err
		}
		m.Invalidate(preserved)
	}
	return unit, nil
}
