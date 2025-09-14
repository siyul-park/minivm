package types

type Value interface {
	Kind() Kind
	Type() Type
	Interface() any
	String() string
}

type Traceable interface {
	Value
	Refs() []Ref
}
