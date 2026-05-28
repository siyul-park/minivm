package types

type Closure struct {
	Typ    *FunctionType
	Fn     int
	Upvals []Boxed
}

var _ Traceable = (*Closure)(nil)

func NewClosure(typ *FunctionType, fn int, upvals []Boxed) *Closure {
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

func (c *Closure) Refs() []Ref {
	refs := make([]Ref, 0, 1+len(c.Upvals))
	refs = append(refs, Ref(c.Fn))
	for _, u := range c.Upvals {
		if u.Kind() == KindRef {
			refs = append(refs, Ref(u.Ref()))
		}
	}
	return refs
}
