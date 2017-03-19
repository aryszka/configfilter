package configfilter

import "github.com/zalando/skipper/eskip"

func uniqueRoutes(r []*eskip.Route) []*eskip.Route {
	var u []*eskip.Route
	for _, ri := range r {
		var found bool
		for _, ui := range u {
			if ui.Id == ri.Id {
				found = true
				break
			}
		}

		if !found {
			u = append(u, ri)
		}
	}

	return u
}

func removeRoutes(a, b []*eskip.Route) []*eskip.Route {
	var c []*eskip.Route
	for _, ai := range a {
		var found bool
		for _, bi := range b {
			if bi.Id == ai.Id {
				found = true
				break
			}
		}

		if !found {
			c = append(c, ai)
		}
	}

	return c
}

func changedRoutes(prev, next []*eskip.Route) []*eskip.Route {
	var changed []*eskip.Route
	for _, pi := range prev {
		for _, ni := range next {
			if ni.Id == pi.Id {
				if ni.String() != pi.String() {
					changed = append(changed, ni)
				}

				break
			}
		}
	}

	return changed
}

func routesToIDs(r []*eskip.Route) []string {
	ids := make([]string, len(r))
	for i, ri := range r {
		ids[i] = ri.Id
	}

	return ids
}

func idsToRoutes(ids []string, from []*eskip.Route) []*eskip.Route {
	var routes []*eskip.Route
	for _, id := range ids {
		for _, r := range from {
			if r.Id == id {
				routes = append(routes, r)
				break
			}
		}
	}

	return routes
}

func replaceRoutes(prev, next []*eskip.Route) ([]*eskip.Route, []*eskip.Route, []string) {
	deletedRoutes := removeRoutes(prev, next)
	insertedRoutes := removeRoutes(next, prev)
	updatedRoutes := changedRoutes(prev, next)
	upserted := append(insertedRoutes, updatedRoutes...)
	deletedIDs := routesToIDs(deletedRoutes)
	return next, upserted, deletedIDs
}

func upsertRoutes(to, from []*eskip.Route) ([]*eskip.Route, []*eskip.Route) {
	insertedRoutes := removeRoutes(from, to)
	updatedRoutes := changedRoutes(to, from)
	upserted := append(insertedRoutes, updatedRoutes...)
	unchangedRoutes := removeRoutes(to, upserted)
	next := append(unchangedRoutes, upserted...)
	return next, upserted
}
