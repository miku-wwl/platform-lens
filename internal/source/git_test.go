package source

import (
	"testing"

	"github.com/miku-wwl/platform-lens/internal/domain"
)

func TestRefCandidatesKeepQualifiedAndUnqualifiedSemantics(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want []refCandidate
	}{
		{name: "qualified branch", ref: "refs/heads/main", want: []refCandidate{{name: "refs/heads/main", kind: domain.RefBranch}}},
		{name: "qualified tag", ref: "refs/tags/v1", want: []refCandidate{{name: "refs/tags/v1", kind: domain.RefTag}}},
		{name: "unqualified", ref: "release", want: []refCandidate{{name: "refs/heads/release", kind: domain.RefBranch}, {name: "refs/tags/release", kind: domain.RefTag}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := refCandidates(tt.ref)
			if len(got) != len(tt.want) {
				t.Fatalf("refCandidates(%q) returned %d candidates, want %d: %#v", tt.ref, len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("refCandidates(%q)[%d]=%#v, want %#v", tt.ref, i, got[i], tt.want[i])
				}
			}
		})
	}
}
