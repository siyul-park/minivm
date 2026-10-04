package violations

import "context"

type Thing struct{} // want "CP001"

func NewThing() interface{} { return Thing{} } // want "CP001" "CP004"

func Use(value int, ctx context.Context) {} // want "CP001" "CP003"

func (Thing) First() {} // want "CP001"

func (Thing) Second() {} // want "CP001" "receiver Thing has methods in multiple files"
