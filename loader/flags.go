package loader

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/danielorbach/go-component"
	"github.com/danielorbach/go-component/loader/loaderflags"
)

var (
	// CommandLine is the default set of command-line flags, parsed from os.Args. The
	// top-level function ParseFlags extends this set with flags for each of the
	// component's flags, then parses the extended set. Packages may use methods of
	// CommandLine directly to define their own flags such as Var() and so on.
	CommandLine = flag.NewFlagSet(ProgramName(), flag.ContinueOnError)

	parsed bool // whether ParseFlags has been called
)

// ParseFlags creates a flag for each of the component's flags, including (in
// "multi" mode) a flag named after the component, parses the flags, then filters
// and returns the list of descriptors enabled by flags.
func ParseFlags(descriptors ...*component.Descriptor) {
	if parsed {
		panic("component/loader: ParseFlags called more than once")
	}

	descriptors, err := loaderflags.ParseFlags(CommandLine, descriptors, true)
	if err != nil {
		InvalidArgument(err.Error())
	}
	Descriptors = descriptors
	parsed = true
}

// InvalidArgument alerts the user about a problem with the program's
// arguments and exits with a non-zero status.
func InvalidArgument(msg string) {
	CommandLine.Usage()
	fmt.Fprintln(CommandLine.Output(), "") // extra newline separator for readability
	fmt.Fprintln(CommandLine.Output(), msg)
	os.Exit(2)
}

// Require marks the component's named flag as required. If the flag is not set
// by the time ParseFlags is called, the program will exit with a non-zero
// status.
func Require(d *component.Descriptor, name string) {
	loaderflags.RequiredFlag(&d.Flags, name, d)
}

// RequireCommandLine marks the named global (i.e. on CommandLine flag-set) flag
// as required. If the flag is not set by the time ParseFlags is called, the
// program will exit with a non-zero status.
func RequireCommandLine(name string) {
	loaderflags.RequiredFlag(CommandLine, name, nil)
}

// ProgramName returns the name of the program, as invoked by the user.
// If the program was invoked as "go run ...", the name of the file being run
// is returned.
func ProgramName() string {
	name := filepath.Base(os.Args[0])
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(name, ".exe")
	}
	return name
}
