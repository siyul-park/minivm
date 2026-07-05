package types

type Closure struct {
	Typ    *FunctionType
	Fn     Ref
	Upvals []Boxed
}

var _ Traceable = (*Closure)(nil)

func NewClosure(typ *FunctionType, fn Ref, upvals []Boxed) *Closure {
	if typ == nil {
		typ = &FunctionType{}
	}
	return &Closure{Typ: typ, Fn: fn, Upvals: upvals}
}

func (c *Closure) Kind() Kind {
	return KindRef
}

func (c *Closure) Type() Type {
	return c.Typ
}

func (c *Closure) String() string {
	return c.Typ.String()
}

func (c *Closure) Refs(dst []Ref) []Ref {
	dst = append(dst, c.Fn)
	for _, u := range c.Upvals {
		if u.Kind() == KindRef {
			dst = append(dst, Ref(u.Ref()))
		}
	}
	return dst
}
