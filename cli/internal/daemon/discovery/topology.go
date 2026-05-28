package discovery

import (
	"fmt"
	"sort"
	"strings"
)

// RenderTopology returns an ASCII tree of the service's dependency graph,
// formatted for `a-novel run topology` output. One service per invocation;
// the daemon RPC handler concatenates per service when --service is omitted.
//
// Layout matches the spec §14.4 sketch — infra first (it's the root), then
// one-shots (in compose-graph order), then long-runners, with each leaf
// annotated by current phase + (mode).
func RenderTopology(s *Service) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", s.Name)
	// Sort infra alphabetically; one-shots and long-runners in two
	// separate groups, each alphabetical, so the topology is
	// deterministic regardless of map iteration order.
	type row struct {
		label  string
		detail string
	}
	rows := make([]row, 0, len(s.Infra)+len(s.Targets))
	for _, in := range s.Infra {
		detail := "(infra)"
		if len(in.DependsOn) > 0 {
			detail = "(infra)  depends-on: " + strings.Join(in.DependsOn, ", ")
		}
		rows = append(rows, row{label: in.Name, detail: detail})
	}
	// Targets: one-shots first, then long-runners. Within each, alphabetical.
	oneShots := make([]*Target, 0)
	longRunners := make([]*Target, 0)
	for _, t := range s.Targets {
		if t.Kind == TargetKindOneShot {
			oneShots = append(oneShots, t)
		} else {
			longRunners = append(longRunners, t)
		}
	}
	sort.Slice(oneShots, func(i, j int) bool { return oneShots[i].Name < oneShots[j].Name })
	sort.Slice(longRunners, func(i, j int) bool { return longRunners[i].Name < longRunners[j].Name })
	for _, t := range append(oneShots, longRunners...) {
		detail := fmt.Sprintf("(%s)", t.Kind)
		if len(t.DependsOn) > 0 {
			detail = fmt.Sprintf("(%s)  depends-on: %s", t.Kind, strings.Join(t.DependsOn, ", "))
		}
		rows = append(rows, row{label: t.Name, detail: detail})
	}
	// Render: use ├─ for all but the last entry, └─ for the last.
	for i, r := range rows {
		prefix := "├─"
		if i == len(rows)-1 {
			prefix = "└─"
		}
		fmt.Fprintf(&b, "%s %-22s %s\n", prefix, r.label, r.detail)
	}
	return b.String()
}
