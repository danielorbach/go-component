package loaderflags

import (
	"bytes"
	"flag"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/danielorbach/go-component"
)

func TestLookupRequired(t *testing.T) {
	// reuse a single flag-set for all tests
	// (because LookupRequired does not mutate it)
	fs := flag.NewFlagSet(t.Name(), flag.ContinueOnError)

	fs.Bool("unbound", false, "not bound to any component")
	RequiredFlag(fs, "unbound", nil)

	fs.Bool("A", false, "bound to component A")
	componentA := &component.Descriptor{Name: "A"}
	RequiredFlag(fs, "A", componentA)

	fs.Bool("B", false, "bound to component B")
	componentB := &component.Descriptor{Name: "B"}
	RequiredFlag(fs, "B", componentB)

	fs.Bool("C", false, "bound to component C")
	componentC := &component.Descriptor{Name: "C"}
	RequiredFlag(fs, "C", componentC)

	tests := []struct {
		name      string
		enabled   []*component.Descriptor
		wantFlags []string
	}{
		{
			name:      "all-enabled",
			enabled:   []*component.Descriptor{componentA, componentB, componentC},
			wantFlags: []string{"A", "B", "C", "unbound"},
		},
		{
			name:      "all-disabled",
			enabled:   []*component.Descriptor{},
			wantFlags: []string{"unbound"},
		},
		{
			name:      "some-enabled",
			enabled:   []*component.Descriptor{componentA, componentC},
			wantFlags: []string{"A", "C", "unbound"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			missing := LookupRequired(fs, tt.enabled)
			gotFlags := make([]string, len(missing))
			for i, f := range missing {
				gotFlags[i] = f.Name
			}

			diff := cmp.Diff(tt.wantFlags, gotFlags, cmpopts.SortMaps(func(x, y string) bool { return x < y }))
			if diff != "" {
				t.Errorf("LookupRequired() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestRequiredFlag makes sure that the RequiredFlag function sets the default
// value of the flag to "required" when displaying the help message.
func TestRequiredFlag_help(t *testing.T) {
	// define a flag and mark it as required
	fs := flag.NewFlagSet(t.Name(), flag.ContinueOnError)
	fs.String("test", "default", "a test flag")
	RequiredFlag(fs, "test", nil)

	// capture the default values of all command line arguments
	var defaults bytes.Buffer
	fs.SetOutput(&defaults)
	fs.PrintDefaults()

	if !strings.Contains(defaults.String(), "default required") {
		t.Error("RequiredFlag did not set the default value to 'required'")
	}

	// in order to display "default required" in the help message, the DefValue field
	// of the flag.Flag must be set to "required". this is achieved by the
	// RequiredFlag function.
	if got := (requiredValue{}.String()); got == "required" {
		t.Log("See comment on requiredValue.String() in required.go")
		t.Errorf("requiredValue.String() = %v, want %v", got, "required")
	}
}
