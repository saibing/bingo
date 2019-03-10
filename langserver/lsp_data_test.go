package langserver

import "golang.org/x/tools/go/packages/packagestest"

var testdata = []packagestest.Module{
	{
		Name: "github.com/saibing/bingo/langserver/test/pkg",
		Files: map[string]interface{}{
			"basic/a.go": `package p; func A() { A() }`,
			"basic/b.go": `package p; func B() { A() }`,

			"builtin/a.go": `package p; func A() { println("hello") }`,

			"detailed/a.go": `package p; type T struct { F string }`,

			"exported_on_unexported/a.go": `package p; type t struct { F string }`,

			"gomodule/a.go": `package a; import "github.com/saibing/dep"; var _ = dep.D; var _ = dep.D`,
			"gomodule/b.go": `package a; import "github.com/saibing/dep/subp"; var _ = subp.D`,
			"gomodule/c.go": `package a; import "github.com/saibing/dep/dep1"; var _ = dep1.D1().D2`,

			"goproject/a/a.go": `package a; func A() {}`,
			"goproject/b/b.go": `package b; import "github.com/saibing/bingo/langserver/test/pkg/goproject/a"; var _ = a.A`,

			"goroot/a.go": `package p; import "fmt"; var _ = fmt.Println; var x int`,

			"implementations/i0.go":    `package p; type I0 interface { M0() }`,
			"implementations/i1.go":    `package p; type I1 interface { M1() }`,
			"implementations/i2.go":    `package p; type I2 interface { M1(); M2() }`,
			"implementations/t0.go":    `package p; type T0 struct{}`,
			"implementations/t1.go":    `package p; type T1 struct {}; func (T1) M1() {}; func (T1) M3(){}`,
			"implementations/t1e.go":   `package p; type T1E struct { T1 }; var _ = (T1E{}).M1`,
			"implementations/t1p.go":   `package p; type T1P struct {}; func (*T1P) M1() {}`,
			"implementations/p2/p2.go": `package p2; type T2 struct{}; func (T2) M1() {}`,

			"lookup/a/a.go": `package a; type A int; func A1() A { var A A = 1; return A }`,
			"lookup/b/b.go": `package b; import "github.com/saibing/bingo/langserver/test/pkg/lookup/a"; func Dummy() a.A { x := a.A1(); return x }`,
			"lookup/c/c.go": `package c; import "github.com/saibing/bingo/langserver/test/pkg/lookup/a"; func Dummy() **a.A { var x **a.A; return x }`,
			"lookup/d/d.go": `package d; import "github.com/saibing/bingo/langserver/test/pkg/lookup/a"; func Dummy() map[string]a.A { var x map[string]a.A; return x }`,

			"multiple/a.go": `package p; func A() { A() }`,
			"multiple/main.go": `// +build ignore

package main;  func B() { p.A(); B() }`,

			"workspace_multiple/a.go": `package p; import "fmt"; var _ = fmt.Println; var x int`,
			"workspace_multiple/b.go": `package p; import "fmt"; var _ = fmt.Println; var y int`,
			"workspace_multiple/c.go": `package p; import "fmt"; var _ = fmt.Println; var z int`,

			"subdirectory/a.go":    `package d; func A() { A() }`,
			"subdirectory/d2/b.go": `package d2; import "github.com/saibing/bingo/langserver/test/pkg/subdirectory"; func B() { d.A(); B() }`,

			"typealias/a.go": `package p; type A struct{ a int }`,
			"typealias/b.go": `package p; type B = A`,

			"unexpected_paths/a.go": `package p; func A() { A() }`,

			"xreferences/a.go": `package p; import "fmt"; var _ = fmt.Println; var x int`,
			"xreferences/b.go": `package p; import "fmt"; var _ = fmt.Println; var y int`,
			"xreferences/c.go": `package p; import "fmt"; var _ = fmt.Println; var z int`,

			"test/a.go":      `package p; var A int`,
			"test/a_test.go": `package p; import "testing"; import "github.com/saibing/bingo/langserver/test/pkg/test/b"; var X = b.B; func TestB(t *testing.T) {}`,
			"test/b/b.go":    `package b; var B int; func C() int { return B };`,
			"test/c/c.go":    `package c; import "github.com/saibing/bingo/langserver/test/pkg/test/b"; var X = b.B;`,

			"xtest/a.go":      `package p; var A int`,
			"xtest/a_test.go": `package p; var X = A`,
			"xtest/b_test.go": `package p; func Y() int { return X }`,
			"xtest/x_test.go": `package p_test; import "github.com/saibing/bingo/langserver/test/pkg/xtest"; var X = p.A`,
			"xtest/y_test.go": `package p_test; func Y() int { return X }`,

			"renaming/a.go": `package p
import "fmt"

func main() {
	str := A()
	fmt.Println(str)
}

func A() string {
	return "test"
}`,

			"renaming/cgo/a.go": `package p
/*
#define _GNU_SOURCE
#include <stdio.h>
*/
import "C"
import "fmt"

func main() {
	str := A()
	fmt.Println(str)
}

func A() string {
	return "test"
}`,

			"symbols/abc.go": `package a

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

type T string`,
			"symbols/bcd.go": `package a

type YZA struct {}

func (y YZA) BCD() {}`,
			"symbols/cde.go": `package a

var(
	a, b string
	c int
)`,
			"symbols/xyz.go": `package a

func yza() {}`,

			"signature/a.go": `package p

// Comments for A
func A(foo int, bar func(baz int) int) int {
	return bar(foo)
}


func B() {}

// Comments for C
func C(x int, y int) int {
	return x+y
}
`,
			"signature/b.go": `package p; func main() { B(); A(); A(0,); A(0); C(1,2); A(,) }`,
			"signature/c.go": `package p; import "fmt"; func test1() { fmt.Printf("%s",)}`,
			"signature/d.go": `package p; import "fmt"; func test2() { fmt.Printf()}`,
			"signature/e.go": `package p; import "fmt"; func test3() { append()}`,

			"issue/223.go": `package main

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
}`,
			"issue/261.go": `package main

import "fmt"

type T string
type TM map[string]T

func main() {
	var tm TM
	for _, t := range tm {
		fmt.Println(t)
	}
}`,

			"docs/a.go": `// Copyright 2015 someone.
// Copyrights often span multiple lines.

// Some additional non-package docs.

// Package p is a package with lots of great things.
package p

import "github.com/saibing/dep/pkg2"

// logit is pkg2.X
var logit = pkg2.X

// T is a struct.
type T struct {
	// F is a string field.
	F string

	// H is a header.
	H pkg2.Header
}

// Foo is the best string.
var Foo string

var (
	// I1 is an int
	I1 = 1

	// I2 is an int
	I2 = 3
)`,
			"docs/q.go": `package p
type T2 struct {
	Q string // Q is a string field.
	// X is documented.
	X int // X has comments.
}`,

			"different/abc.go": `package a
type XYZ struct {}`,
			"different/bcd.go": `package a
func (x XYZ) ABC() {}`,

			"completion/a.go": `package p

import "strings"

func s2() {
	_ = strings.Title("s")
	_ = new(strings.Replacer)
}

const s1 = 42

var s3 int
var s4 func()`,
			"completion/b.go": `package p; import "fmt"; var _ = fmt.Printl`,
			"completion/c.go": `package p;

import (
	"fmt"
)

func main() {
	fmt.Println("hahah")
	defer fmt.
}`,
		},
	},
}
