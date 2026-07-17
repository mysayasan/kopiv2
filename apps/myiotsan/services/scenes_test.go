package services

import (
	"context"
	"fmt"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
)

// fakeIssuer records every command it is asked to issue and can be told to refuse specific devices,
// standing in for CommandService so a scene run is testable without a broker or a database.
type fakeIssuer struct {
	calls     []IssueRequest
	devices   []int64
	refuseDev int64 // a device id whose commands are refused (0 = refuse none)
}

func (f *fakeIssuer) Issue(_ context.Context, deviceId int64, req IssueRequest, _ int64, _ string) (*entities.DeviceCommand, error) {
	f.calls = append(f.calls, req)
	f.devices = append(f.devices, deviceId)
	if deviceId == f.refuseDev {
		return &entities.DeviceCommand{Status: "failed", Error: "actuation is not enabled for this device"}, fmt.Errorf("refused")
	}
	return &entities.DeviceCommand{Status: "sent"}, nil
}

func sceneActions() []*entities.SceneAction {
	return []*entities.SceneAction{
		{Ordinal: 0, DeviceId: 10, CommandName: "power", Value: 1},
		{Ordinal: 1, DeviceId: 20, CommandName: "brightness", Value: 40},
		{Ordinal: 2, DeviceId: 30, CommandName: "power", Value: 0},
	}
}

// A scene runs its actions in order, one Issue per action.
func TestScene_RunsEveryActionInOrder(t *testing.T) {
	f := &fakeIssuer{}
	s := &SceneService{commands: f, logf: func(string, ...any) {}}

	res := s.runActions(context.Background(), 1, sceneActions(), 7, "alice")

	if len(f.calls) != 3 {
		t.Fatalf("expected 3 commands issued, got %d", len(f.calls))
	}
	if f.devices[0] != 10 || f.devices[1] != 20 || f.devices[2] != 30 {
		t.Fatalf("actions ran out of order: %v", f.devices)
	}
	if f.calls[1].Name != "brightness" || f.calls[1].Value != 40 {
		t.Fatalf("action 2 wrong: %+v", f.calls[1])
	}
	if len(res.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(res.Results))
	}
	for _, r := range res.Results {
		if r.Status != "sent" {
			t.Errorf("action on %d should be sent, got %q", r.DeviceId, r.Status)
		}
	}
}

// A refused action does NOT abort the rest — partial failure is first-class, and the report shows
// exactly which action failed and why.
func TestScene_PartialFailureDoesNotAbortTheRest(t *testing.T) {
	f := &fakeIssuer{refuseDev: 20} // the middle action is refused
	s := &SceneService{commands: f, logf: func(string, ...any) {}}

	res := s.runActions(context.Background(), 1, sceneActions(), 7, "alice")

	if len(f.calls) != 3 {
		t.Fatalf("all 3 actions must still be attempted, got %d", len(f.calls))
	}
	if res.Results[0].Status != "sent" || res.Results[2].Status != "sent" {
		t.Errorf("the actions around the failure must still succeed: %+v", res.Results)
	}
	if res.Results[1].Status != "failed" || res.Results[1].Error == "" {
		t.Errorf("the refused action must be reported failed with a reason: %+v", res.Results[1])
	}
}
