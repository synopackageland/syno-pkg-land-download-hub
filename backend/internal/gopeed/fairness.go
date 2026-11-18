package gopeed

import "slices"

// fairJobIDs orders candidates by their owner's active/selected work, rotating
// ties after the last admitted owner. Each owner's IDs keep their stable order.
// The caller passes only runnable candidates and accounts for stopping work.
func fairJobIDs(jobs map[string]Job, active map[string]int, after string) []string {
	queues := map[string][]string{}
	counts := map[string]int{}
	for owner, n := range active {
		counts[owner] = n
	}
	for _, id := range sortedIDs(jobs) {
		owner := jobs[id].Owner
		queues[owner] = append(queues[owner], id)
	}
	owners := make([]string, 0, len(queues))
	for owner := range queues {
		owners = append(owners, owner)
	}
	slices.Sort(owners)
	out := make([]string, 0, len(jobs))
	for len(out) < len(jobs) {
		chosen := ""
		found := false
		// Visit owners strictly after the cursor, then wrap around.
		for pass := 0; pass < 2; pass++ {
			for _, owner := range owners {
				if (owner > after) != (pass == 0) || len(queues[owner]) == 0 {
					continue
				}
				if !found || counts[owner] < counts[chosen] {
					chosen, found = owner, true
				}
			}
		}
		out = append(out, queues[chosen][0])
		queues[chosen] = queues[chosen][1:]
		counts[chosen]++
		after = chosen
	}
	return out
}
