package component

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestMergeTargets(t *testing.T) {
	type args struct {
		targets [][]string
	}
	tests := []struct {
		name        string
		args        args
		wantTargets []string
	}{
		{
			name:        "len(targets)=0,n(targets)=0",
			args:        args{},
			wantTargets: nil,
		},
		{
			name:        "len(targets)=1,n(targets)=0",
			args:        args{targets: [][]string{{}}},
			wantTargets: nil,
		},
		{
			name:        "len(targets)=2,n(targets)=0",
			args:        args{targets: [][]string{{}, {}}},
			wantTargets: nil,
		},
		{
			name:        "len(targets)=1,n(targets)=3",
			args:        args{targets: [][]string{{"T1", "T2", "T3"}}},
			wantTargets: []string{"T1", "T2", "T3"},
		},
		{
			name:        "len(targets)=2,n(targets)=3",
			args:        args{targets: [][]string{{"T1", "T2"}, {"T3"}}},
			wantTargets: []string{"T1", "T2", "T3"},
		},
		{
			name:        "len(targets)=3,n(targets)=3",
			args:        args{targets: [][]string{{"T1"}, {"T2"}, {"T3"}}},
			wantTargets: []string{"T1", "T2", "T3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTargets := MergeTargets(tt.args.targets...)
			if diff := cmp.Diff(gotTargets, tt.wantTargets); diff != "" {
				t.Error("MergeTargets() differs\n", diff)
			}
		})
	}
}
