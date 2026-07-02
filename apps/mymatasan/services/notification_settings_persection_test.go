package services

import (
	"context"
	"testing"
)

// Reuses fakeRuntimeSettingRepo from setup_state_test.go (same package).
func newNotifSettingsServiceForTest() *notificationSettingsService {
	return &notificationSettingsService{
		repo:     &fakeRuntimeSettingRepo{},
		notif:    nil, // Save skips hub Configure when nil
		defaults: normalizeNotificationSettings(NotificationSettings{}),
	}
}

// TestSaveDestinationIsolatesOtherSections is the whole point of per-section save:
// persisting one destination must not clobber another destination's stored config
// or the retention section — even if the caller only ever sends the one section.
func TestSaveDestinationIsolatesOtherSections(t *testing.T) {
	s := newNotifSettingsServiceForTest()
	ctx := context.Background()

	// Seed two saved destinations + a retention policy via the full Save path.
	seeded, err := s.Save(ctx, NotificationSettings{
		Retention: NotificationRetentionSettings{Days: 30, IntervalHours: 6},
		Destinations: []NotificationDestination{
			{Id: "a", Name: "A", Type: DestinationTypeWebhook, Enabled: true, URL: "https://a.example.com"},
			{Id: "b", Name: "B", Type: DestinationTypeWebhook, Enabled: true, URL: "https://b.example.com"},
		},
	})
	if err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	if len(seeded.Destinations) != 2 {
		t.Fatalf("seeded destinations = %d, want 2", len(seeded.Destinations))
	}

	// Now save ONLY destination A (changed URL). Destination B and retention must
	// remain exactly as persisted.
	_, after, err := s.SaveDestination(ctx, NotificationDestination{
		Id: "a", Name: "A", Type: DestinationTypeWebhook, Enabled: true, URL: "https://a-new.example.com",
	})
	if err != nil {
		t.Fatalf("SaveDestination: %v", err)
	}
	byID := map[string]NotificationDestination{}
	for _, d := range after.Destinations {
		byID[d.Id] = d
	}
	if byID["a"].URL != "https://a-new.example.com" {
		t.Errorf("destination A not updated: %q", byID["a"].URL)
	}
	if byID["b"].URL != "https://b.example.com" {
		t.Errorf("destination B was clobbered: %q", byID["b"].URL)
	}
	if after.Retention.Days != 30 {
		t.Errorf("retention was clobbered: days = %d, want 30", after.Retention.Days)
	}
}

// TestSaveDestinationAssignsIdOnCreate checks a destination with no id is created
// (appended) with a fresh id, leaving existing ones intact.
func TestSaveDestinationAssignsIdOnCreate(t *testing.T) {
	s := newNotifSettingsServiceForTest()
	ctx := context.Background()
	if _, err := s.Save(ctx, NotificationSettings{
		Destinations: []NotificationDestination{{Id: "a", Name: "A", Type: DestinationTypeWebhook}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	dest, after, err := s.SaveDestination(ctx, NotificationDestination{Name: "New", Type: DestinationTypeWebhook})
	if err != nil {
		t.Fatalf("SaveDestination create: %v", err)
	}
	if dest.Id == "" {
		t.Error("created destination has no id")
	}
	if len(after.Destinations) != 2 {
		t.Fatalf("expected 2 destinations after create, got %d", len(after.Destinations))
	}
}

// TestSaveRetentionKeepsDestinations checks retention-only save leaves the stored
// destinations untouched.
func TestSaveRetentionKeepsDestinations(t *testing.T) {
	s := newNotifSettingsServiceForTest()
	ctx := context.Background()
	if _, err := s.Save(ctx, NotificationSettings{
		Retention:    NotificationRetentionSettings{Days: 7, IntervalHours: 6},
		Destinations: []NotificationDestination{{Id: "a", Name: "A", Type: DestinationTypeWebhook, Enabled: true, URL: "https://a.example.com"}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	after, err := s.SaveRetention(ctx, NotificationRetentionSettings{Days: 90, IntervalHours: 12})
	if err != nil {
		t.Fatalf("SaveRetention: %v", err)
	}
	if after.Retention.Days != 90 || after.Retention.IntervalHours != 12 {
		t.Errorf("retention not updated: %+v", after.Retention)
	}
	if len(after.Destinations) != 1 || after.Destinations[0].URL != "https://a.example.com" {
		t.Errorf("destinations clobbered by retention save: %+v", after.Destinations)
	}
}

// TestDeleteDestinationRemovesOnlyTarget checks delete removes just the target.
func TestDeleteDestinationRemovesOnlyTarget(t *testing.T) {
	s := newNotifSettingsServiceForTest()
	ctx := context.Background()
	if _, err := s.Save(ctx, NotificationSettings{
		Destinations: []NotificationDestination{
			{Id: "a", Name: "A", Type: DestinationTypeWebhook},
			{Id: "b", Name: "B", Type: DestinationTypeWebhook},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	after, err := s.DeleteDestination(ctx, "a")
	if err != nil {
		t.Fatalf("DeleteDestination: %v", err)
	}
	if len(after.Destinations) != 1 || after.Destinations[0].Id != "b" {
		t.Errorf("delete removed wrong rows: %+v", after.Destinations)
	}
}
