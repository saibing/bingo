package main

import "fmt"

type T string
type TM map[string]T

func main() {
	var tm TM
	for _, t := range tm {
		fmt.Println(t)
	}
}
