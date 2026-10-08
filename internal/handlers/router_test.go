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

// The route table is hand-written and the paths are the contract the Lab app's
// adapter is keyed on, so a typo is a 404 nobody sees until a screen is empty.
// This pins the paths added for the QA/lab module rather than trusting review.
func TestNewRouterRegistersLabModuleRoutes(t *testing.T) {
	registered := map[string]bool{}
	for _, route := range NewRouter(RouterDeps{}).Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, want := range []string{
		"GET /api/v1/calibrations",
		"POST /api/v1/calibrations",
		"GET /api/v1/calibrations/:id",
		"GET /api/v1/stability-studies",
		"POST /api/v1/stability-studies",
		"GET /api/v1/non-conformances",
		"POST /api/v1/non-conformances",
		"GET /api/v1/non-conformances/:id",
		"GET /api/v1/in-process-checks",
		"POST /api/v1/in-process-checks",
		"GET /api/v1/release-decisions",
		"POST /api/v1/release-decisions",
		"GET /api/v1/hold-events",
		"POST /api/v1/hold-events",
		"GET /api/v1/lab/measurements",
		"POST /api/v1/samples/:id/measurements",
		"GET /api/v1/incoming-inspections",
		"POST /api/v1/incoming-inspections",
		"DELETE /api/v1/lab/methods/:id",
		"DELETE /api/v1/lab/requests/:id",
		"DELETE /api/v1/stability-studies/:id",
		"DELETE /api/v1/in-process-checks/:id",
		"DELETE /api/v1/incoming-inspections/:id",
	} {
		if !registered[want] {
			t.Errorf("route not registered: %s", want)
		}
	}
}
