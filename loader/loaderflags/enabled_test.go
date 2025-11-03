package loaderflags

import (
	"flag"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/danielorbach/go-component"
)

func TestComponentSet(t *testing.T) {
	var components = map[string]*component.Descriptor{
		"A": {Name: "A"},
		"B": {Name: "B"},
		"C": {Name: "C"},
	}

	// tests always contain the same components, but
	// the arguments vary to enable different components.
	tests := []struct {
		name string
		args []string
		want []*component.Descriptor
	}{
		{
			name: "unset",
			want: []*component.Descriptor{components["A"], components["B"], components["C"]},
		},
		{
			name: "all-enabled",
			args: []string{"-A=true", "-B=true", "-C=true"},
			want: []*component.Descriptor{components["A"], components["B"], components["C"]},
		},
		{
			name: "all-enabled-boolflag",
			args: []string{"-A", "-B", "-C"},
			want: []*component.Descriptor{components["A"], components["B"], components["C"]},
		},
		{
			name: "all-disabled",
			args: []string{"-A=false", "-B=false", "-C=false"},
			want: []*component.Descriptor{},
		},
		{
			name: "some-enabled",
			args: []string{"-A=true", "-C"},
			want: []*component.Descriptor{components["A"], components["C"]},
		},
		{
			name: "some-disabled",
			args: []string{"-B=false"},
			want: []*component.Descriptor{components["A"], components["C"]},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// register the components with the flag set
			cs := make(ComponentSet)
			fs := flag.NewFlagSet(t.Name(), flag.ContinueOnError)
			for _, d := range components {
				cs.Register(fs, d)
			}

			// parse the arguments
			if err := fs.Parse(tt.args); err != nil {
				t.Fatalf("Parse(%v) = %v", tt.args, err)
			}

			// check that the enabled components match the expected
			got := cs.Enabled()
			extractName := cmp.Transformer("name", func(d *component.Descriptor) string { return d.Name })
			if diff := cmp.Diff(tt.want, got, extractName); diff != "" {
				t.Errorf("Enabled() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
