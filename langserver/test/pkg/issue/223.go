package main

import (
	"fmt"
)

func main() {

	b := &Hello{
		a: 1,
	}

	fmt.Println(b.Bye())
}

type Hello struct {
	a int
}

func (h *Hello) Bye() int {
	return h.a
}