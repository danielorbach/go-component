package fileloader_test

import (
	"log/slog"

	"github.com/danielorbach/go-component"
	"github.com/danielorbach/go-component/fileloader"
	"github.com/danielorbach/go-component/loader"
)

func ExampleMain() {
	// Component is an imported descriptor for the purpose of this example.
	var Component = &component.Descriptor{
		Name: "example",
		Bootstrap: func(l *component.L, _ component.Linker, _ any) error {
			slog.InfoContext(l.Context(), "Hello world!")
			return nil
		},
		OptionsType: nil,
	}

	// simply call fileloader.Main with the component descriptors of choice, and it
	// shall parse the commandline and load the components described by the footprint
	// provided as the first positional argument.
	fileloader.Main(Component)
}

func ExampleLoad() {
	// Component is an imported descriptor for the purpose of this example.
	var Component = &component.Descriptor{
		Name: "example",
		Bootstrap: func(l *component.L, _ component.Linker, _ any) error {
			slog.InfoContext(l.Context(), "Hello world!")
			return nil
		},
		OptionsType: nil,
	}

	// sometimes the footprint is embedded in the binary, and we can use the
	// fileloader.Load function to load it directly from memory.
	const Footprint = `{
	  "Name": "Example Footprint",
	  "Metadata": "This footprint is serialized into bytes and then unmarshalled into loader.Footprint.",
	  "Identifier": "00000000-abcd-0000-abcd-000000000000",
	  "Locations": null,
	  "Components": {
		"example": {
		  "Location": "",
		  "Options": null,
		  "Aspects": null,
		  "Interests": null
		}
	  }
	}`

	// in which case, we must parse the commandline flags ourselves before attempting
	// to load the footprint. fortunately, this is easy to do:
	loader.ParseFlags(Component)
	// then we can call fileloader.Load with the footprint as a byte slice, and the
	// format of the footprint (JSON, YAML, etc.) - in this case, JSON.
	fileloader.Load([]byte(Footprint), fileloader.FormatJSON)
}
