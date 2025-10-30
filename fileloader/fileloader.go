// Package fileloader defines the main function for a component loader with only
// a constant footprint at runtime. This package makes it easy for a provider of
// component subsystem packages to also provide a standalone tool that loads just
// those descriptors from a hard-coded footprint - usually for demonstration
// purposes.
//
// For example, if github.com/example/my-system is a component package, exposing one
// or more component descriptors, all that is needed to define a standalone
// demonstration tool is a file, github.com/example/my-system/cmd/demo/main.go,
// containing:
//
//	// The demo command runs a demonstration deployment of the new subsystem.
//	Package main
//
//	import (
//		"github.com/example/my-system"
//		"github.com/danielorbach/go-component/fileloader"
//		"github.com/danielorbach/go-component/loader"
//	)
//
//	const footprint = `...`
//
//	func main() {
//		loader.ParseFlags(newsubsystem.ComponentA, newsubsystem.ComponentB, ...)
//		fileloader.Load(footprint, fileloader.FormatYAML)
//	}
package fileloader

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/danielorbach/go-component"
	"github.com/danielorbach/go-component/loader"
)

// Stdin is a special value for the footprint argument, indicating that the
// footprint should be read from stdin.
const Stdin = "-"

// FormatFlag is a flag.Value that specifies the format of the footprint, and
// thus the appropriate loader to use.
type FormatFlag string

const (
	// FormatAuto indicates that the footprint format should be inferred; currently
	// from the file extension.
	FormatAuto FormatFlag = "auto"

	// FormatYAML indicates that the footprint is in YAML format.
	FormatYAML FormatFlag = "yaml"
	// FormatJSON indicates that the footprint is in JSON format.
	FormatJSON FormatFlag = "json"
)

func (f *FormatFlag) String() string {
	if f == nil {
		return ""
	}
	return string(*f)
}

func (f *FormatFlag) Set(s string) error {
	switch FormatFlag(s) {
	case FormatYAML, FormatJSON, FormatAuto:
		*f = FormatFlag(s)
	default:
		return fmt.Errorf("unknown format: %q", s)
	}
	return nil
}

// Main parses the commandline and loads the components described by the
// footprint.
func Main(d ...*component.Descriptor) {
	// Override the default usage message to include the footprint positional argument.
	loader.CommandLine.Usage = func() {
		fmt.Fprintf(loader.CommandLine.Output(), "Usage: %s [options] footprint\n", loader.CommandLine.Name())
		loader.CommandLine.PrintDefaults()
	}

	var format = FormatAuto
	loader.CommandLine.Var(&format, "footprint-format", "format of the footprint: yaml, json, auto")

	loader.ParseFlags(d...)
	if loader.CommandLine.NArg() != 1 {
		loader.InvalidArgument("missing required footprint argument")
	}
	name := loader.CommandLine.Arg(0)

	// if the format is auto, try to guess the format from the file extension
	if format == FormatAuto {
		switch {
		case strings.HasSuffix(name, ".yaml"), strings.HasSuffix(name, ".yml"):
			format = FormatYAML
		case strings.HasSuffix(name, ".json"):
			format = FormatJSON
		case name == Stdin:
			// special case error message for stdin
			loader.InvalidArgument("cannot auto-detect format from stdin")
		default:
			loader.InvalidArgument("cannot auto-detect format from file extension")
		}
	}

	// read the footprint from the file or stdin
	var data []byte
	if name == Stdin {
		p, err := io.ReadAll(os.Stdin)
		if err != nil {
			loader.InvalidArgument("cannot read stdin: " + err.Error())
		}
		data = p
	} else {
		p, err := os.ReadFile(name)
		if err != nil {
			loader.InvalidArgument("cannot read file: " + err.Error())
		}
		data = p
	}
	Load(data, format)
}

// Load loads the components described by the footprint, using the specified
// format to decode it.
func Load(footprint []byte, format FormatFlag) {
	if !loader.CommandLine.Parsed() {
		panic("component/fileloader: loader.ParseFlags must be called")
	}

	switch format {
	case FormatYAML:
		fp, err := YAMLLoader(footprint).footprint()
		if err != nil {
			loader.InvalidArgument("cannot load footprint: " + err.Error())
		}
		loader.Load(fp, component.WithSpan("loader"))
	case FormatJSON:
		fp, err := UnmarshalFootprintJSON(footprint)
		if err != nil {
			loader.InvalidArgument("cannot load footprint: " + err.Error())
		}
		loader.Load(fp, component.WithSpan("loader"))
	case FormatAuto:
		panic("component/fileloader: cannot auto-detect format from bytes")
	default:
		panic("component/fileloader: unknown format: " + format)
	}
}

func validateTargets(d *component.Descriptor, aspects, interests map[string]string) error {
	// pre-hash the aspects and interests lists of the descriptor for fast lookup
	var lookupAspects = make(map[string]struct{}, len(d.Aspects))
	for _, a := range d.Aspects {
		lookupAspects[a] = struct{}{}
	}
	var lookupInterests = make(map[string]struct{}, len(d.Interests))
	for _, i := range d.Interests {
		lookupInterests[i] = struct{}{}
	}

	var errs []error

	// find aspects defined in the descriptor that are not in the footprint
	for a := range lookupAspects {
		if _, ok := aspects[a]; !ok {
			errs = append(errs, fmt.Errorf("footprint missing aspect %q", a))
		}
	}
	// find aspects defined in the footprint that are not in the descriptor
	for a := range aspects {
		if _, ok := lookupAspects[a]; !ok {
			errs = append(errs, fmt.Errorf("descriptor missing aspect %q", a))
		}
	}

	// find interests defined in the descriptor that are not in the footprint
	for _, i := range d.Interests {
		if _, ok := interests[i]; !ok {
			errs = append(errs, fmt.Errorf("footprint missing interest %q", i))
		}
	}
	// find interests defined in the footprint that are not in the descriptor
	for i := range interests {
		if _, ok := lookupInterests[i]; !ok {
			errs = append(errs, fmt.Errorf("descriptor missing interest %q", i))
		}
	}

	return errors.Join(errs...)
}
