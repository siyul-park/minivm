package pass

import (
	"fmt"
	"reflect"

	"github.com/siyul-park/minivm/program"
)

type Manager struct {
	passes map[reflect.Type][]reflect.Value
	cache  map[reflect.Type]reflect.Value
}

var (
	ErrPassInvalid      = fmt.Errorf("invalid pass type")
	ErrPassUnregistered = fmt.Errorf("registered pass type")
)

func NewManager() *Manager {
	return &Manager{
		passes: make(map[reflect.Type][]reflect.Value),
		cache:  make(map[reflect.Type]reflect.Value),
	}
}

func (m *Manager) Register(pass any) error {
	val := reflect.ValueOf(pass)
	run, ok := val.Type().MethodByName("Run")
	if !ok ||
		run.Type.NumIn() != 2 || run.Type.In(1) != reflect.TypeOf(m) ||
		run.Type.NumOut() != 2 || run.Type.Out(1) != reflect.TypeOf((*error)(nil)).Elem() {
		return ErrPassInvalid
	}
	m.passes[run.Type.Out(0)] = append(m.passes[run.Type.Out(0)], val)
	return nil
}

func (m *Manager) Load(val any) error {
	v := reflect.ValueOf(val)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return ErrPassUnregistered
	}

	typ := v.Elem().Type()

	r, ok := m.cache[typ]
	if !ok {
		passes, ok := m.passes[typ]
		if !ok {
			return ErrPassUnregistered
		}
		for _, p := range passes {
			run := p.MethodByName("Run")
			res := run.Call([]reflect.Value{reflect.ValueOf(m)})
			if err := res[1]; !err.IsNil() {
				return err.Interface().(error)
			}
			r = res[0]
			m.cache[typ] = r
		}
	}

	v.Elem().Set(r)
	return nil
}

func (m *Manager) Run(prog *program.Program) error {
	progType := reflect.TypeOf(prog)
	m.cache = map[reflect.Type]reflect.Value{
		progType: reflect.ValueOf(prog),
	}
	for _, p := range m.passes[progType] {
		run := p.MethodByName("Run")
		res := run.Call([]reflect.Value{reflect.ValueOf(m)})
		if err := res[1]; !err.IsNil() {
			return err.Interface().(error)
		}
		if !res[0].IsZero() && res[0].IsValid() {
			m.cache = map[reflect.Type]reflect.Value{
				progType: res[0],
			}
		}
	}
	return nil
}
