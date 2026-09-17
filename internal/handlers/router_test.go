package handlers

import "testing"

// gin builds its radix tree at registration time and panics on two wildcard
// names in the same position (e.g. "/batches/:batchId/lab" next to
// "/batches/:businessId"). Handlers only touch their deps per request, so
// zero-value deps are enough to prove the route table itself is well-formed.
func TestNewRouterBuilds(t *testing.T) {
	r := NewRouter(RouterDeps{})
	if len(r.Routes()) == 0 {
		t.Fatal("expected routes to be registered")
	}
}
