package loaderflags

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/peterbourgon/ff/v3"

	"github.com/danielorbach/go-component"
)

// ErrNoComponents is returned by ParseFlags when no components are enabled.
var ErrNoComponents = errors.New("no enabled components")

// ParseFlags defines a flag (on the provides flag-set) for each of the
// component's flags; including, in 'multi' mode, a flag to enable the component.
// Flag values are parsed from the command line and environment variables.
//
// A 'help' command is added to the flag-set, which prints usage information for
// all components, and then exits. Otherwise, the parsed values are propagated
// into each component's flags and the enabled components are returned.
func ParseFlags(cmdline *flag.FlagSet, descriptors []*component.Descriptor, multi bool) ([]*component.Descriptor, error) {
	// TODO: find a way to discover and auto-register GraphChanged nodes (implementing Value) with gob.

	// connect each component flag to the command line as -component.flag
	components := make(ComponentSet, len(descriptors))
	for _, d := range descriptors {
		var prefix string

		// add "-NAME" flag to enable it
		if multi {
			prefix = d.Name + "."
			// register component switch flag
			components.Register(cmdline, d)
		}

		d.Flags.VisitAll(func(f *flag.Flag) {
			name := prefix + f.Name // note that prefix is empty if !multi
			// duplicate flag names usually indicate a developer error
			if cmdline.Lookup(name) != nil {
				log.Panicf("%s flag -%s would conflict with loader; aborting", d.Name, f.Name)
			}
			cmdline.Var(f.Value, name, f.Usage)
		})
	}

	// flags common to all loaders
	// TODO: add common flags (debug, profiling)
	//CommandLine.BoolVar(&JSON, "json", JSON, "emit JSON output")
	//CommandLine.IntVar(&Context, "c", Context, `display offending line with this many lines of context`)

	err := ff.Parse(cmdline, os.Args[1:], ff.WithEnvVarSplit(";"), ff.WithEnvVars())
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(cmdline.Output(), err)
		os.Exit(2)
	}

	// show additional help, if requested
	if cmdline.Arg(0) == "help" {
		Help(cmdline, cmdline.Name(), descriptors)
		os.Exit(0)
	}

	// if any "-NAME" flag is true, load only those components; otherwise, if any
	// "-NAME" flag is false, load all but those components.
	if multi {
		descriptors = components.Enabled()
	}

	if len(descriptors) == 0 {
		return nil, ErrNoComponents
	}

	// fail if any required flags are missing
	missing := LookupRequired(cmdline, descriptors)
	if len(missing) > 0 {
		var required error
		for _, f := range missing {
			required = errors.Join(required, fmt.Errorf("flag -%s ($%s) is required", f.Name, ffEnvName(f.Name, "")))
		}
		return nil, required
	}

	return descriptors, nil
}

// ffEnvName returns the environment variable name for a given flag name,
// based on the implementation of ff.Parse (v3.3.0).
func ffEnvName(name string, prefix string) string {
	var flagNameToEnvVar = strings.NewReplacer(
		"-", "_",
		".", "_",
		"/", "_",
	)

	key := strings.ToUpper(name)
	key = flagNameToEnvVar.Replace(key)
	if prefix != "" {
		key = strings.ToUpper(prefix) + "_" + key
	}
	return key
}
