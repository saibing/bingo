package a

type XYZ struct {}

func (x XYZ) ABC() {}

var (
	A = 1
)

const (
	B = 2
)

type (
	_ struct{}
	C struct{}
)

type UVW interface {}

type T string