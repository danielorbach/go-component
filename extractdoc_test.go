// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package component_test

import (
	"testing"

	"github.com/danielorbach/go-component"
)

func TestExtractDoc(t *testing.T) {
	const multi = `// Copyright

//+build tag

// Package foo
//
// # Irrelevant heading
//
// This is irrelevant doc.
//
// # Service nocolon
//
// This one has the wrong form for this line.
//
// # Service food
//
// food: reports dining opportunities
//
// This is the doc for service 'food'.
//
// # Service foo
//
// foo: reports diagnostics
//
// This is the doc for service 'foo'.
//
// # Service bar
//
// bar: reports drinking opportunities
//
// This is the doc for service 'bar'.
package blah

var x = syntax error
`

	for _, test := range []struct {
		content, name string
		want          string // doc or "error: %w" string
	}{
		{"", "foo",
			"error: empty Go source file"},
		{"//foo", "foo",
			"error: not a Go source file"},
		{"//foo\npackage foo", "foo",
			"error: package doc comment contains no 'Service foo' heading"},
		{multi, "foo",
			"reports diagnostics\n\nThis is the doc for service 'foo'."},
		{multi, "bar",
			"reports drinking opportunities\n\nThis is the doc for service 'bar'."},
		{multi, "food",
			"reports dining opportunities\n\nThis is the doc for service 'food'."},
		{multi, "nope",
			"error: package doc comment contains no 'Service nope' heading"},
		{multi, "nocolon",
			"error: 'Service nocolon' heading not followed by 'nocolon: summary...' line"},
	} {
		got, err := component.ExtractDoc(test.content, test.name)
		if err != nil {
			got = "error: " + err.Error()
		}
		if test.want != got {
			t.Errorf("ExtractDoc(%q) returned <<%s>>, want <<%s>>, given input <<%s>>",
				test.name, got, test.want, test.content)
		}
	}
}
