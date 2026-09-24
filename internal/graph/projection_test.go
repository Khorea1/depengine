package graph

import (
	"errors"
	"strings"
	"testing"
)

func TestProjectDeclaredCanonicalizesWithoutContext(t *testing.T) {
	g := NewGraph()
	g.AddEdge(Edge{From: "z", To: "app", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "a", To: "app", Kind: ToolRequire, Role: Scheduling})

	projected, err := g.Project(DeclaredView, ProjectionContext{})
	if err != nil {
		t.Fatalf("declared projection failed: %v", err)
	}
	if projected.Edges[0].From != "a" || projected.Edges[1].From != "z" {
		t.Fatalf("declared projection is not canonical: %#v", projected.Edges)
	}
}

func TestProjectEffectiveEvaluatesOnlyGuardedEdges(t *testing.T) {
	g := NewGraph()
	g.AddNode(Node{ID: "app"})
	g.AddNode(Node{ID: "always"})
	g.AddNode(Node{ID: "active"})
	g.AddNode(Node{ID: "inactive"})
	g.AddEdge(Edge{From: "always", To: "app", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "active", To: "app", Kind: ToolRequire, Role: Scheduling, Guard: testGuard("yes")})
	g.AddEdge(Edge{From: "inactive", To: "app", Kind: ToolRequire, Role: Scheduling, Guard: testGuard("no")})

	calls := 0
	projected, err := g.Project(EffectiveView, ProjectionContext{
		GuardActive: func(guard Guard) (bool, error) {
			calls++
			return guard.String() == "yes", nil
		},
	})
	if err != nil {
		t.Fatalf("effective projection failed: %v", err)
	}
	if calls != 2 {
		t.Fatalf("guard evaluator called %d times, want 2", calls)
	}
	if len(projected.Edges) != 2 {
		t.Fatalf("effective projection edges = %#v", projected.Edges)
	}
	for _, edge := range projected.Edges {
		if edge.From == "inactive" {
			t.Fatalf("inactive edge survived effective projection: %#v", projected.Edges)
		}
	}
}

func TestProjectEffectiveNeedsEvaluatorOnlyForGuards(t *testing.T) {
	unguarded := NewGraph()
	unguarded.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling})
	if _, err := unguarded.Project(EffectiveView, ProjectionContext{}); err != nil {
		t.Fatalf("unguarded effective graph unexpectedly needs context: %v", err)
	}

	guarded := NewGraph()
	guarded.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling, Guard: testGuard("guarded")})
	_, err := guarded.Project(EffectiveView, ProjectionContext{})
	if !errors.Is(err, ErrProjectionUnavailable) {
		t.Fatalf("guarded effective Project error = %v, want ErrProjectionUnavailable", err)
	}
}

func TestProjectPropagatesGuardEvaluationError(t *testing.T) {
	g := NewGraph()
	g.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling, Guard: testGuard("bad")})
	boom := errors.New("boom")
	_, err := g.Project(EffectiveView, ProjectionContext{
		GuardActive: func(Guard) (bool, error) { return false, boom },
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Project error = %v, want wrapped evaluator error", err)
	}
}

func TestProjectResolvedFiltersMethodEdges(t *testing.T) {
	g := NewGraph()
	g.AddNode(Node{ID: "app"})
	g.AddEdge(Edge{From: "base", To: "app", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "curl", To: "app", Kind: MethodRequire, Role: Activation, Method: "http", Candidate: 0, CandidateKnown: true})
	g.AddEdge(Edge{From: "git", To: "app", Kind: MethodRequire, Role: Activation, Method: "source", Candidate: 1, CandidateKnown: true})

	projected, err := g.Project(ResolvedView, ProjectionContext{
		SelectedCandidate: func(toolID string, candidate int) bool {
			if toolID != "app" {
				t.Fatalf("selector called for unexpected tool %q", toolID)
			}
			return candidate == 0
		},
	})
	if err != nil {
		t.Fatalf("resolved projection failed: %v", err)
	}
	if len(projected.Edges) != 2 {
		t.Fatalf("resolved edges = %#v", projected.Edges)
	}
	for _, edge := range projected.Edges {
		if edge.Kind == MethodRequire && edge.Method != "http" {
			t.Fatalf("unselected method edge survived: %#v", projected.Edges)
		}
	}
}

func TestProjectResolvedRejectsUnknownCandidateIdentity(t *testing.T) {
	g := NewGraph()
	g.AddEdge(Edge{From: "curl", To: "app", Kind: MethodRequire, Role: Activation, Method: "http"})
	_, err := g.Project(ResolvedView, ProjectionContext{
		SelectedCandidate: func(string, int) bool { return true },
	})
	if !errors.Is(err, ErrProjectionUnavailable) {
		t.Fatalf("resolved Project error = %v, want ErrProjectionUnavailable for unknown candidate identity", err)
	}
}

func TestProjectResolvedRequiresSelectorForMethodEdges(t *testing.T) {
	g := NewGraph()
	g.AddEdge(Edge{From: "curl", To: "app", Kind: MethodRequire, Role: Activation, Method: "http", Candidate: 0, CandidateKnown: true})
	_, err := g.Project(ResolvedView, ProjectionContext{})
	if !errors.Is(err, ErrProjectionUnavailable) {
		t.Fatalf("resolved Project error = %v, want ErrProjectionUnavailable", err)
	}
}

func TestProjectResolvedDropsMethodEdgeWhenNoSelectionExists(t *testing.T) {
	g := NewGraph()
	g.AddEdge(Edge{From: "curl", To: "app", Kind: MethodRequire, Role: Activation, Method: "http", Candidate: 0, CandidateKnown: true})
	projected, err := g.Project(ResolvedView, ProjectionContext{
		SelectedCandidate: func(string, int) bool { return false },
	})
	if err != nil {
		t.Fatalf("resolved projection failed: %v", err)
	}
	if len(projected.Edges) != 0 {
		t.Fatalf("expected no resolved method edges, got %#v", projected.Edges)
	}
}

func TestProjectResolvedDistinguishesSameKindCandidates(t *testing.T) {
	g := NewGraph()
	g.AddNode(Node{ID: "app"})
	g.AddEdge(Edge{From: "curl", To: "app", Kind: MethodRequire, Role: Activation, Method: "http", Candidate: 0, CandidateKnown: true})
	g.AddEdge(Edge{From: "wget", To: "app", Kind: MethodRequire, Role: Activation, Method: "http", Candidate: 1, CandidateKnown: true})

	projected, err := g.Project(ResolvedView, ProjectionContext{
		SelectedCandidate: func(toolID string, candidate int) bool {
			return toolID == "app" && candidate == 1
		},
	})
	if err != nil {
		t.Fatalf("resolved projection failed: %v", err)
	}
	if len(projected.Edges) != 1 || projected.Edges[0].From != "wget" || projected.Edges[0].Candidate != 1 {
		t.Fatalf("resolved projection kept the wrong same-kind candidate edges: %#v", projected.Edges)
	}
}

func TestProjectResolvedSkipsUnselectedGuardWithoutEvaluator(t *testing.T) {
	g := NewGraph()
	g.AddNode(Node{ID: "app"})
	g.AddEdge(Edge{From: "curl", To: "app", Kind: MethodRequire, Role: Activation, Method: "http", Candidate: 0, CandidateKnown: true})
	g.AddEdge(Edge{From: "wget", To: "app", Kind: MethodRequire, Role: Activation, Method: "http", Candidate: 1, CandidateKnown: true, Guard: testGuard("linux")})

	projected, err := g.Project(ResolvedView, ProjectionContext{
		SelectedCandidate: func(toolID string, candidate int) bool {
			return toolID == "app" && candidate == 0
		},
	})
	if err != nil {
		t.Fatalf("unselected guarded candidate should not require guard context: %v", err)
	}
	if len(projected.Edges) != 1 || projected.Edges[0].From != "curl" {
		t.Fatalf("unexpected resolved edges: %#v", projected.Edges)
	}
}

func TestProjectRejectsUnknownView(t *testing.T) {
	_, err := NewGraph().Project(GraphView(255), ProjectionContext{})
	if err == nil {
		t.Fatal("expected unknown projection to fail")
	}
	if got := GraphView(255).String(); got != "unknown(255)" {
		t.Fatalf("unexpected unknown view label: %q", got)
	}
}

func TestProjectionContextErrorIncludesEdge(t *testing.T) {
	g := NewGraph()
	g.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling, Guard: testGuard("x")})
	_, err := g.Project(EffectiveView, ProjectionContext{
		GuardActive: func(Guard) (bool, error) { return false, errors.New("invalid facts") },
	})
	if err == nil || !strings.Contains(err.Error(), `"a"`) || !strings.Contains(err.Error(), `"b"`) || !strings.Contains(err.Error(), "invalid facts") {
		t.Fatalf("unexpected projection error: %v", err)
	}
}
