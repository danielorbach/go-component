// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package component

import (
	"fmt"
	"go/parser"
	"go/token"
	"strings"
)

// MustExtractDoc is like [ExtractDoc] but it panics on error.
//
// to use, define a doc.go file in your package that contains a
// package comment with a heading for each service's component
// descriptor; like this:
//
//	// Package healthcheck defines a simple component of a subsystem.
//	//
//	// # Service healthcheck
//	//
//	// healthcheck: reports whether execution is alive.
//	//
//	// The healthcheck component reports a diagnostic for services
//	// that run forever; to pass the diagnostics, try responding to
//	// pings as fast as possible.
//	package healthcheck
//
//	import _ "embed"
//
//	//go:embed doc.go
//	var doc string
//
// And declare your component as:
//
//	var Component = &component.Descriptor{
//		Name:             "healthcheck",
//		Doc:              loader.MustExtractDoc(doc, "healthcheck"),
//		...
//	}
func MustExtractDoc(content, name string) string {
	doc, err := ExtractDoc(content, name)
	if err != nil {
		panic(err)
	}
	return doc
}

// ExtractDoc extracts a section of a package doc comment from the
// provided contents of a service component package's doc.go file.
//
// A section is a portion of the comment between one heading and
// the next, using this form:
//
//	# Service NAME
//
//	NAME: SUMMARY
//
//	Full description...
//
// where NAME matches the name argument, and SUMMARY is a brief
// verb-phrase that describes the service. The following lines, up
// until the next heading or the end of the comment, contain the full
// description. ExtractDoc returns the portion following the colon,
// which is the form expected by Descriptor.Doc.
//
// Example:
//
//	# Service hello-world
//
//	hello-world: consistently echoes hello-world to the console
//
//	The hello-world service demonstrates a long-running service.
//	Here is the complete description...
//
// This notation allows a single doc comment to provide documentation
// for multiple services, each in its own section.
// The HTML anchors generated for each heading are predictable.
//
// It returns an error if the content was not a valid Go source file
// containing a package doc comment with a heading of the required
// form.
//
// This machinery enables the package documentation (typically
// accessible via the web at https://pkg.go.dev/) and the command
// documentation (typically printed to a terminal) to be derived from
// the same source and formatted appropriately.
func ExtractDoc(content, name string) (string, error) {
	if content == "" {
		return "", fmt.Errorf("empty Go source file")
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", content, parser.ParseComments|parser.PackageClauseOnly)
	if err != nil {
		return "", fmt.Errorf("not a Go source file")
	}
	if f.Doc == nil {
		return "", fmt.Errorf("the Go source file has no package doc comment")
	}
	for _, section := range strings.Split(f.Doc.Text(), "\n# ") {
		if body := strings.TrimPrefix(section, "Service "+name); body != section &&
			body != "" &&
			body[0] == '\r' || body[0] == '\n' {
			body = strings.TrimSpace(body)
			rest := strings.TrimPrefix(body, name+":")
			if rest == body {
				return "", fmt.Errorf("'Service %s' heading not followed by '%s: summary...' line", name, name)
			}
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("package doc comment contains no 'Service %s' heading", name)
}
