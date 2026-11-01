package types

// IteratorType is the type of iterators yielding Elem.
type IteratorType struct {
	Elem Type
}

var _ Type = (*IteratorType)(nil)

// NewIteratorType returns the iterator type yielding elem.
func NewIteratorType(elem Type) *IteratorType {
	return &IteratorType{Elem: elem}
}

func (t *IteratorType) Kind() Kind {
	return KindRef
}

func (t *IteratorType) Cast(other Type) bool {
	return t.Equals(other)
}

func (t *IteratorType) Equals(other Type) bool {
	if t == other {
		return true
	}
	o, ok := other.(*IteratorType)
	if !ok {
		return false
	}
	return t.Elem.Equals(o.Elem)
}

func (t *IteratorType) String() string {
	return "iterator[" + t.Elem.String() + "]"
}
