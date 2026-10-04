package violations

import "context"

type Thing struct{} // want "CP001"

type privateThing struct{}

func Use(value int, ctx context.Context) {} // want "CP001" "CP003"

func NewThing() interface{} { return Thing{} } // want "CP001" "CP004"

type laterThing struct{} // want "CP002"

// UseHelperA exercises a cross-group dependency that file ordering does not constrain.
func UseHelperA() { helperValue() }

// UseHelperB exercises the same shared lower-level helper.
func UseHelperB() { helperValue() }

func (Thing) First() {} // want "CP001"

func (Thing) Second() {} // want "CP001" "receiver Thing has methods in multiple files"

// helperValue is shared by two higher-level functions.
func helperValue() {}
