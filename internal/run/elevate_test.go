package run

import (
	"context"
	"reflect"
	"testing"
)

// Forced elevation wins on the instance even when the test process runs as
// root (e.g. inside a container), and Override("") drops the forced state
// so the next call re-probes the host.
func TestElevatorOverrideAndReset(t *testing.T) {
	e := NewElevator()
	e.Override("run0")
	if got := e.Prefix(); !reflect.DeepEqual(got, []string{"run0"}) {
		t.Fatalf("Prefix() = %v, want [run0] while overridden", got)
	}
	if got := e.Method(); got != "run0" {
		t.Fatalf("Method() = %q, want run0 while overridden", got)
	}

	e.Override("")
	if got := e.Method(); got != detectElevation() {
		t.Fatalf("Method() after reset = %q, want a fresh probe %q", got, detectElevation())
	}
}

// Override on one instance must not leak into another: no shared globals.
func TestElevatorInstancesAreIndependent(t *testing.T) {
	a, b := NewElevator(), NewElevator()
	a.Override("pkexec")
	if b.overridden || b.probed {
		t.Fatalf("fresh instance shares state with an overridden one (overridden=%v probed=%v)", b.overridden, b.probed)
	}
	if got := a.Method(); got != "pkexec" {
		t.Fatalf("Method() = %q, want pkexec", got)
	}
}

// RunElevated through an overridden instance prefixes argv without a shell.
func TestElevatorRunElevatedPrefixesArgv(t *testing.T) {
	e := NewElevator()
	e.Override("sudo")
	fake := &FakeRunner{}
	_ = e.RunElevated(context.Background(), fake, "apt-get", "update")
	if len(fake.Calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(fake.Calls))
	}
	call := fake.Calls[0]
	if call.Name != "sudo" || !reflect.DeepEqual(call.Args, []string{"apt-get", "update"}) {
		t.Fatalf("call = %v %v, want sudo [apt-get update]", call.Name, call.Args)
	}
}
