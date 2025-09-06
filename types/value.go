package types

type Value interface {
	Interface() any
	String() string
}
