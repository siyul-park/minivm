package types

type String string

type stringType struct{}

var TypeString = stringType{}

var _ Value = String("")
var _ Type = stringType{}

func (s String) Kind() Kind {
	return KindRef
}

func (s String) Type() Type {
	return TypeString
}

func (s String) Interface() any {
	return string(s)
}

func (s String) String() string {
	return string(s)
}
func (s stringType) Kind() Kind {
	return KindRef
}

func (s stringType) String() string {
	return "string"
}

func (s stringType) Equals(other Type) bool {
	return other == TypeString
}
