package p

// Comments for A
func A(foo int, bar func(baz int) int) int {
	return bar(foo)
}


func B() {}

// Comments for C
func C(x int, y int) int {
	return x+y
}
