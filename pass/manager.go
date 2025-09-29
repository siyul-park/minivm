package pass

import (
	"fmt"
	"reflect"

	"github.com/siyul-park/minivm/program"
)

type Manager struct {
	passes map[reflect.Type]reflect.Value
	cache  map[reflect.Type]reflect.Value
}

var (
	ErrInvalid  = fmt.Errorf("invalid argument")
	ErrNotFound = fmt.Errorf("not found")
)

func NewManager() *Manager {
	return &Manager{
		passes: make(map[reflect.Type]reflect.Value),
		cache:  make(map[reflect.Type]reflect.Value),
	}
}

func (m *Manager) Register(pass any) error {
	val := reflect.ValueOf(pass)
	run, ok := val.Type().MethodByName("Run")
	if !ok ||
		run.Type.NumIn() != 2 || run.Type.In(1) != reflect.TypeOf(m) ||
		run.Type.NumOut() != 2 || run.Type.Out(1) != reflect.TypeOf((*error)(nil)).Elem() {
		return ErrInvalid
	}
	m.passes[run.Type.Out(0)] = val
	return nil
}

func (m *Manager) Load(val any) error {
	v := reflect.ValueOf(val)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return ErrInvalid
	}

	typ := v.Elem().Type()

	r, ok := m.cache[typ]
	if !ok {
		p, ok := m.passes[typ]
		if !ok {
			return ErrNotFound
		}

		run := p.MethodByName("Run")
		res := run.Call([]reflect.Value{reflect.ValueOf(m)})

		if err := res[1]; !err.IsNil() {
			return err.Interface().(error)
		}

		r = res[0]
		m.cache[typ] = r
	}

	v.Elem().Set(r)
	return nil
}

func (m *Manager) Run(prog *program.Program) error {
	m.cache = map[reflect.Type]reflect.Value{
		reflect.TypeOf(prog): reflect.ValueOf(prog),
	}
	for t := range m.passes {
		val := reflect.New(t).Interface()
		if err := m.Load(val); err != nil {
			return err
		}
	}
	return nil
}
