package services

import (
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
)

func TestCommand_SwitchTakesOnlyZeroOrOne(t *testing.T) {
	decl := &entities.ProfileCommand{Name: "output", Kind: "switch"}
	if err := validateValue(decl, 1); err != nil {
		t.Fatalf("1 is valid for a switch: %v", err)
	}
	if err := validateValue(decl, 0); err != nil {
		t.Fatalf("0 is valid for a switch: %v", err)
	}
	if err := validateValue(decl, 2); err == nil {
		t.Fatal("2 is not a switch position and must be refused")
	}
	if err := validateValue(decl, -1); err == nil {
		t.Fatal("-1 is not a switch position and must be refused")
	}
}

// The bounds are a SAFETY property, enforced here rather than in a form. A thermostat that
// accepts 200 degrees because a UI slider was bypassed is a fire.
func TestCommand_SetpointIsBoundedServerSide(t *testing.T) {
	decl := &entities.ProfileCommand{Name: "setpoint", Kind: "setpoint", Min: 5, Max: 30}

	if err := validateValue(decl, 21); err != nil {
		t.Fatalf("21 is inside 5..30: %v", err)
	}
	if err := validateValue(decl, 5); err != nil {
		t.Fatal("the boundary itself is allowed")
	}
	if err := validateValue(decl, 200); err == nil {
		t.Fatal("200 degrees is outside the safe range and must be REFUSED, not clamped")
	}
	if err := validateValue(decl, -40); err == nil {
		t.Fatal("-40 is outside the safe range and must be refused")
	}
	// The refusal has to be actionable: "outside the safe range 5..30" tells an operator what to
	// do; "bad request" does not.
	err := validateValue(decl, 200)
	if !strings.Contains(err.Error(), "5") || !strings.Contains(err.Error(), "30") {
		t.Fatalf("the refusal must name the safe range, got %q", err)
	}
}

// An unbounded setpoint is an OMISSION, not permission. The safe reading of an omission on a
// physical device is no.
func TestCommand_SetpointWithNoDeclaredRangeRefusesEverything(t *testing.T) {
	decl := &entities.ProfileCommand{Name: "setpoint", Kind: "setpoint"} // no Min, no Max

	for _, v := range []float64{0, 1, 21, 1000} {
		if err := validateValue(decl, v); err == nil {
			t.Fatalf("a setpoint with no declared safe range must accept nothing, but accepted %v", v)
		}
	}
}

func TestCommand_PayloadTemplateSubstitutesTheValue(t *testing.T) {
	decl := &entities.ProfileCommand{
		PayloadTemplate: `{"method":"Switch.Set","params":{"id":0,"on":{value}}}`,
	}
	got := renderPayload(decl, 1)
	want := `{"method":"Switch.Set","params":{"id":0,"on":1}}`
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestCommand_EmptyTemplateSendsTheBareValue(t *testing.T) {
	if got := renderPayload(&entities.ProfileCommand{}, 21.5); got != "21.5" {
		t.Fatalf("a device whose topic IS the instruction gets the bare value, got %q", got)
	}
}

// Numbers must not arrive at a device in scientific notation or with float noise. A relay that
// receives "1e+00" instead of "1" does nothing, and the operator is left staring at a command
// that was "sent" and never took effect.
func TestCommand_ValuesRenderCleanly(t *testing.T) {
	cases := map[float64]string{1: "1", 0: "0", 21.5: "21.5", 100000: "100000", 0.1: "0.1"}
	for v, want := range cases {
		if got := trimNum(v); got != want {
			t.Fatalf("%v must render as %q, got %q", v, want, got)
		}
	}
}
