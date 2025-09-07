package types

type Value interface {
	Interface() any
	String() string
}

type Traceable interface {
	Value
	Refs() []Ref
}
