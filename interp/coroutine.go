package interp

import "github.com/siyul-park/minivm/types"

// coroutine is a single suspended frame captured by a YIELD inside a called
// function. Calling a coroutine-function (one whose body contains a YIELD)
// produces a coroutine handle instead of plain return values; RESUME continues
// it, delivering a value back as the result of the pending YIELD. A coroutine
// owns one reference to its function, captured stack image, upvalues, and last
// value, reported via Refs so the collector keeps them live while suspended.
type coroutine struct {
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

var _ types.Traceable = (*coroutine)(nil)

func (c *coroutine) Kind() types.Kind { return types.KindRef }

func (c *coroutine) Type() types.Type {
	if c.typ == nil {
		return &types.FunctionType{}
	}
	return c.typ
}

func (c *coroutine) String() string {
	return "coroutine"
}

func (c *coroutine) Refs(dst []types.Ref) []types.Ref {
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
