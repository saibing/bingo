// Copyright 2015 someone.
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
)