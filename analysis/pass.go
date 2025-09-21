package analysis

type Pass interface {
	Run(m *Module) error
}

type PassManager struct {
	passes []Pass
}

var _ Pass = (*PassManager)(nil)

func NewPassManager() *PassManager {
	return &PassManager{}
}

func (pm *PassManager) Add(pass Pass) {
	pm.passes = append(pm.passes, pass)
}

func (pm *PassManager) Run(m *Module) error {
	for _, p := range pm.passes {
		if err := p.Run(m); err != nil {
			return err
		}
	}
	return nil
}
