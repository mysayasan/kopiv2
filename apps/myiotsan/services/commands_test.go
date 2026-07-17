package services

import (
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
)

// encodeRegister is the risky part of the Modbus write path — it turns a human value into the raw
// register word, applying the read scale in reverse. Getting it wrong writes a wrong number to real
// hardware, so it is pinned here; the full Issue→write→read-back path is exercised live against the
// simulator (which honours writes).
func TestCommand_EncodeRegisterScalesAndRangeChecks(t *testing.T) {
	cases := []struct {
		kind    string
		scale   float64
		value   float64
		want    uint16
		wantErr bool
	}{
		{"u16", 1, 50, 50, false},
		{"u16", 0.1, 60, 600, false}, // raw = 60 / 0.1
		{"", 1, 50, 50, false},       // empty kind defaults to u16
		{"u16", 0, 42, 42, false},    // scale 0 treated as 1
		{"i16", 1, -5, 65531, false}, // two's-complement bit pattern of int16(-5)
		{"i16", 1, -32768, 32768, false},
		{"u16", 1, 70000, 0, true}, // out of u16 range
		{"u16", 1, -1, 0, true},    // negative into u16
		{"i16", 1, 40000, 0, true}, // out of i16 range
		{"u32", 1, 5, 0, true},     // multi-register kinds refused, not half-written
	}
	for _, c := range cases {
		got, err := encodeRegister(c.kind, c.scale, c.value)
		if c.wantErr {
			if err == nil {
				t.Errorf("encodeRegister(%q,%v,%v) expected an error", c.kind, c.scale, c.value)
			}
			continue
		}
		if err != nil {
			t.Errorf("encodeRegister(%q,%v,%v) unexpected error: %v", c.kind, c.scale, c.value, err)
		} else if got != c.want {
			t.Errorf("encodeRegister(%q,%v,%v) = %d, want %d", c.kind, c.scale, c.value, got, c.want)
		}
	}
}

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

// The home-automation kinds. dimmer/position are 0..100 percentages.
func TestCommand_DimmerAndPositionAreBounded0to100(t *testing.T) {
	for _, kind := range []string{"dimmer", "position"} {
		decl := &entities.ProfileCommand{Kind: kind}
		for _, v := range []float64{0, 50, 100} {
			if err := validateValue(decl, v); err != nil {
				t.Errorf("%s: %v should be valid: %v", kind, v, err)
			}
		}
		for _, v := range []float64{-1, 101, 254} {
			if err := validateValue(decl, v); err == nil {
				t.Errorf("%s: %v is outside 0..100 and must be refused", kind, v)
			}
		}
	}
}

// cct is a setpoint in Kelvin: bounded, and an unbounded one refuses everything (an omission is no).
func TestCommand_CctIsABoundedSetpoint(t *testing.T) {
	decl := &entities.ProfileCommand{Kind: "cct", Min: 2200, Max: 6500}
	if err := validateValue(decl, 4000); err != nil {
		t.Fatalf("4000K is inside 2200..6500: %v", err)
	}
	if err := validateValue(decl, 10000); err == nil {
		t.Fatal("10000K is outside the range and must be refused")
	}
	if err := validateValue(&entities.ProfileCommand{Kind: "cct"}, 4000); err == nil {
		t.Fatal("a cct with no declared range must accept nothing")
	}
}

// mode accepts only the integer values its Options enumerate; an empty Options accepts nothing.
func TestCommand_ModeAcceptsOnlyDeclaredOptions(t *testing.T) {
	decl := &entities.ProfileCommand{Kind: "mode", Options: `[{"value":0,"label":"Off"},{"value":2,"label":"Cool"},{"value":3,"label":"Heat"}]`}
	for _, v := range []float64{0, 2, 3} {
		if err := validateValue(decl, v); err != nil {
			t.Errorf("mode %v is declared and must be accepted: %v", v, err)
		}
	}
	if err := validateValue(decl, 1); err == nil {
		t.Fatal("mode 1 is not declared and must be refused")
	}
	if err := validateValue(&entities.ProfileCommand{Kind: "mode"}, 0); err == nil {
		t.Fatal("a mode with no options must accept nothing")
	}
}

// color must be a whole number in 0..0xFFFFFF, and packs/unpacks losslessly through one float.
func TestCommand_ColorIsAPackedRGBInteger(t *testing.T) {
	decl := &entities.ProfileCommand{Kind: "color"}
	if err := validateValue(decl, packRGB(255, 128, 0)); err != nil {
		t.Fatalf("orange is a valid colour: %v", err)
	}
	if err := validateValue(decl, 0x1000000); err == nil {
		t.Fatal("a value above 0xFFFFFF must be refused")
	}
	if err := validateValue(decl, 100.5); err == nil {
		t.Fatal("a fractional colour must be refused")
	}
	for _, c := range [][3]int{{0, 0, 0}, {255, 255, 255}, {12, 200, 77}, {255, 128, 0}} {
		r, g, b := unpackRGB(packRGB(c[0], c[1], c[2]))
		if r != c[0] || g != c[1] || b != c[2] {
			t.Errorf("pack/unpack round-trip broke: %v -> %d,%d,%d", c, r, g, b)
		}
	}
}

// The default is a REFUSAL: an unknown kind must not be published unvalidated (the old silent hole).
func TestCommand_UnknownKindIsRefused(t *testing.T) {
	if err := validateValue(&entities.ProfileCommand{Kind: "teleport"}, 1); err == nil {
		t.Fatal("an unknown command kind must be refused, not silently passed")
	}
}

// A colour command renders {r}/{g}/{b} from the packed value, alongside {value}.
func TestCommand_ColorPayloadSubstitutesChannels(t *testing.T) {
	decl := &entities.ProfileCommand{Kind: "color", PayloadTemplate: `{"color":{"r":{r},"g":{g},"b":{b}}}`}
	got := renderPayload(decl, packRGB(255, 128, 0))
	want := `{"color":{"r":255,"g":128,"b":0}}`
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}
