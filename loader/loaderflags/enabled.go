package loaderflags

import (
	"flag"
	"fmt"
	"sort"
	"strconv"

	"github.com/danielorbach/go-component"
)

// A ComponentSet tracks which of its registered components are enabled by
// command-line flags.
type ComponentSet map[*component.Descriptor]*triState

// Register the given component as a tri-state flag on the given flag-set. The
// flag will be named after the component's name (see triStateFlag).
//
// If the component is already registered, this is a no-op.
func (cs ComponentSet) Register(fs *flag.FlagSet, d *component.Descriptor) {
	if _, ok := cs[d]; ok {
		return
	}
	cs[d] = triStateFlag(fs, d.Name)
}

// Enabled returns the set of components that the given flags enable, in lexical
// order by component name.
//
// If any "-NAME" flag is true, load only those components; otherwise, if any
// "-NAME" flag is false, load all but those components.
func (cs ComponentSet) Enabled() []*component.Descriptor {
	var hasTrue, hasFalse bool
	for _, f := range cs {
		switch *f {
		case triStateTrue:
			hasTrue = true
		case triStateFalse:
			hasFalse = true
		default: // keep hasTrue, hasFalse zero-values
		}
	}

	keep := make([]*component.Descriptor, 0, len(cs))
	if hasTrue { // if any flag is true, load only those components that are true
		for d, f := range cs {
			if *f == triStateTrue {
				keep = append(keep, d)
			}
		}
	} else if hasFalse { // if any flag is false, load all but those components that are false
		for d, f := range cs {
			if *f != triStateFalse {
				keep = append(keep, d)
			}
		}
	} else { // else load all components
		for d := range cs {
			keep = append(keep, d)
		}
	}

	// sort by component name for deterministic output
	sort.Slice(keep, func(i, j int) bool { return keep[i].Name < keep[j].Name })
	return keep
}

// A triState is a boolean that knows whether it has been set to either true or
// false. It is used to identify whether a flag appears; the standard boolean
// flag cannot distinguish missing from unset. It also satisfies flag.Value
// interface.
//
// triState implements flag.Value, flag.Getter, and flag.boolFlag. They work like
// boolean flags: we can say "/my-bin -printf" as well as "/my-bin -printf=true"
type triState int

const (
	triStateUnset triState = iota
	triStateTrue
	triStateFalse
)

func triStateFlag(fs *flag.FlagSet, name string) *triState {
	f := new(triState)
	usage := "enable loading " + name + " component"
	fs.Var(f, name, usage)
	return f
}

func (f *triState) Get() interface{} {
	return *f == triStateTrue
}

func (f *triState) Set(value string) error {
	b, err := strconv.ParseBool(value)
	if err != nil {
		// This error message looks poor, but package "flag" adds
		// "invalid boolean value %q for -NAME: %s"
		return fmt.Errorf("want true or false")
	}
	if b {
		*f = triStateTrue
	} else {
		*f = triStateFalse
	}
	return nil
}

func (f *triState) String() string {
	switch *f {
	case triStateUnset:
		return "unset"
	case triStateTrue:
		return "true"
	case triStateFalse:
		return "false"
	}
	panic("unreachable")
}

func (f *triState) IsBoolFlag() bool {
	return true
}
