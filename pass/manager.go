package pass

import (
	"fmt"
	"reflect"
)

type Manager struct {
	parent *Manager
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

func (m *Manager) Convert(src, dst any) error {
	child := &Manager{
		parent: m,
		passes: m.passes,
		cache:  make(map[reflect.Type]reflect.Value),
	}
	if err := child.Run(src); err != nil {
		return err
	}
	return child.Load(dst)
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
			if p := m.parent; p != nil {
				for p := m.parent; p != nil; p = p.parent {
					if err := p.Load(val); err == nil {
						return nil
					}
				}
			}
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

func (m *Manager) Run(val any) error {
	typ := reflect.TypeOf(val)
	m.cache = map[reflect.Type]reflect.Value{
		typ: reflect.ValueOf(val),
	}
	for _, p := range m.passes[typ] {
		run := p.MethodByName("Run")
		res := run.Call([]reflect.Value{reflect.ValueOf(m)})
		if err := res[1]; !err.IsNil() {
			return err.Interface().(error)
		}
		if !res[0].IsZero() && res[0].IsValid() {
			m.cache = map[reflect.Type]reflect.Value{
				typ: res[0],
			}
		}
	}
	return nil
}
