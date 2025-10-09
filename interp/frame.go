package interp

import "github.com/siyul-park/minivm/types"

type frame struct {
	fn   *types.Function
	addr int
	ip   int
	bp   int
}
