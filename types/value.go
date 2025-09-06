package types

type Value interface {
	Kind() Kind
	Interface() any
}
