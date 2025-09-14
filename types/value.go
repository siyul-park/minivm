package types

type Value interface {
	Kind() Kind
	Interface() any
	String() string
}

type Traceable interface {
	Value
	Refs() []Ref
}
