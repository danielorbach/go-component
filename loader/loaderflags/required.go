package loaderflags

import (
	"flag"
	"sort"
	"strconv"

	"github.com/danielorbach/go-component"
)

// LookupRequired returns the flags from the given flag-set that were required
// prior (by RequiredFlag) but have not been set by the command-line. The
// returned list is sorted lexicographically by the flag's name.
//
// The lookup takes into account the provided set of component descriptors such
// that only some required flags are ignored if their bound components are not
// provided.
func LookupRequired(fs *flag.FlagSet, descriptors []*component.Descriptor) (missing []flag.Flag) {
	components := make(map[*component.Descriptor]struct{}, len(descriptors))
	for _, d := range descriptors {
		components[d] = struct{}{}
	}

	omitted := make(map[*flag.Flag]struct{})
	fs.VisitAll(func(f *flag.Flag) {
		omitted[f] = struct{}{}
	})
	fs.Visit(func(f *flag.Flag) {
		delete(omitted, f)
	})

	for f := range omitted {
		if rv, ok := f.Value.(requiredValue); ok {
			if rv.component == nil { // global flag
				missing = append(missing, *f)
			} else if _, enabled := components[rv.component]; enabled { // flag for enabled component
				missing = append(missing, *f)
			}
		}
	}

	// sort by flag name for deterministic output
	sort.Slice(missing, func(i, j int) bool { return missing[i].Name < missing[j].Name })
	return missing
}

// RequiredFlag wraps the value of the named flag such that its help message
// shows "default required" and indicates to ParseFlags it should fail unless the
// flag has been set explicitly (ignoring its predefined default value).
func RequiredFlag(fs *flag.FlagSet, name string, boundComponent *component.Descriptor) {
	f := fs.Lookup(name)
	if f == nil {
		panic("component/loader: cannot require " + strconv.Quote(name) + " flag from this flag-set")
	}

	f.Value = requiredValue{Value: f.Value, component: boundComponent}
	// the flag.Flag already has a DefValue set, but we want to display
	// "default required" in the help message, so we overwrite it here.
	f.DefValue = "required"
}

type requiredValue struct {
	flag.Value
	// some requiredFlags are bound to a component, so that they should fail
	// ParseFlags only if that component is enabled.
	component *component.Descriptor
}

// String may be called on a zero requiredValue, which will panic because the
// Value will have been nil. Also, any default set on the internal Value should
// be ignored - a requiredValue flag always gets a value set anyway.
//
// In order to display "default required" in the help message, the DefValue field
// of the flag.Flag must be set to "required". This is achieved by either the
// RequiredFlag function above or by calling this String method when the flag is
// created (most likely by flag.Var during ParseFlags).
func (r requiredValue) String() string {
	if r.Value == nil {
		// this value should never be displayed, it is only returned
		// by calling String on a zero-value requiredValue - which is
		// only done by the flag package when generating the help message.
		return "THIS IS ZERO-VALUE REQUIRED FLAG"
	}
	return "required"
}
