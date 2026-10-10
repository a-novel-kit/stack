package discovery

import (
	"fmt"
	"strings"
)

// RenderTopology returns an ASCII tree of one service's dependency graph, as
// `a-novel run topology` prints it. The daemon RPC handler concatenates the
// per-service renders when --service is omitted.
//
// Infra comes first as the root, then one-shots, then long-runners, with each
// line annotated by its kind and its depends_on list.
func RenderTopology(s *Service) string {
	type row struct {
		label string
		kind  string
		deps  []string
	}
	rows := make([]row, 0, len(s.Infra)+len(s.Targets))
	for _, in := range s.Infra {
		rows = append(rows, row{label: in.Name, kind: "infra", deps: in.DependsOn})
	}
	// One-shots first, then long-runners, each group in discovery's name order.
	for _, oneShots := range []bool{true, false} {
		for _, t := range s.Targets {
			if (t.Kind == TargetKindOneShot) == oneShots {
				rows = append(rows, row{label: t.Name, kind: t.Kind.String(), deps: t.DependsOn})
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", s.Name)
	for i, r := range rows {
		prefix := "├─"
		if i == len(rows)-1 {
			prefix = "└─"
		}
		detail := "(" + r.kind + ")"
		if len(r.deps) > 0 {
			detail += "  depends-on: " + strings.Join(r.deps, ", ")
		}
		fmt.Fprintf(&b, "%s %-22s %s\n", prefix, r.label, detail)
	}
	return b.String()
}
