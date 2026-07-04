package interp

import "github.com/siyul-park/minivm/types"

// Coroutine is a single suspended frame captured by a YIELD inside a called
// function. Calling a coroutine-function (one whose body contains a YIELD)
// produces a Coroutine handle instead of plain return values; RESUME continues
// it, delivering a value back as the result of the pending YIELD. A Coroutine
// owns one reference to its function, captured stack image, upvalues, and last
// value, reported via Refs so the collector keeps them live while suspended.
type Coroutine struct {
	typ *types.FunctionType

	image  []types.Boxed
	upvals []types.Boxed

	value types.Boxed

	addr    int
	ref     int
	returns int
	ip      int

	release bool
	done    bool
}

var _ types.Traceable = (*Coroutine)(nil)

func (c *Coroutine) Kind() types.Kind { return types.KindRef }

func (c *Coroutine) Type() types.Type {
	if c.typ == nil {
		return &types.FunctionType{}
	}
	return c.typ
}

func (c *Coroutine) String() string {
	return "coroutine"
}

func (c *Coroutine) Refs(dst []types.Ref) []types.Ref {
	if c.ref > 0 {
		dst = append(dst, types.Ref(c.ref))
	}
	for _, v := range c.image {
		if v.Kind() == types.KindRef {
			dst = append(dst, types.Ref(v.Ref()))
		}
	}
	for _, v := range c.upvals {
		if v.Kind() == types.KindRef {
			dst = append(dst, types.Ref(v.Ref()))
		}
	}
	if c.value.Kind() == types.KindRef {
		dst = append(dst, types.Ref(c.value.Ref()))
	}
	return dst
}
