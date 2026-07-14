package services

import (
	"testing"
	"time"
)

func TestEnrollment_ClosedWindowVerifiesNothing(t *testing.T) {
	e := NewEnrollment(nil, nil, nil)
	if e.VerifyKey("anything") {
		t.Fatal("a window that was never opened must admit nobody")
	}
	if e.Status().Open {
		t.Fatal("status must report closed")
	}
}

func TestEnrollment_OpenAdmitsOnlyTheMintedKey(t *testing.T) {
	e := NewEnrollment(nil, nil, nil)
	st, err := e.Open(t.Context(), 10*time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if st.Key == "" {
		t.Fatal("opening a window must return the key exactly once")
	}
	if !e.VerifyKey(st.Key) {
		t.Fatal("the minted key must be accepted")
	}
	if e.VerifyKey(st.Key + "x") {
		t.Fatal("a near-miss key must be refused")
	}
	if e.VerifyKey("") {
		t.Fatal("an empty password must be refused — there is no default")
	}
}

// The key must never be readable after it is minted. An enrollment key that can be fetched
// back out of the API is a permanent credential wearing a temporary hat.
func TestEnrollment_StatusNeverRevealsTheKey(t *testing.T) {
	e := NewEnrollment(nil, nil, nil)
	if _, err := e.Open(t.Context(), 10*time.Minute, 1); err != nil {
		t.Fatal(err)
	}
	if got := e.Status(); got.Key != "" {
		t.Fatalf("Status must never carry the key, got %q", got.Key)
	}
}

// The expiry is what makes this a WINDOW rather than a permanently open door with a nicer name.
// It is enforced on every connect, not by a sweeper that might not have run.
func TestEnrollment_ExpiredWindowStopsAdmitting(t *testing.T) {
	e := NewEnrollment(nil, nil, nil)
	st, err := e.Open(t.Context(), time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !e.VerifyKey(st.Key) {
		t.Fatal("valid while open")
	}

	// Wind the clock past the expiry.
	e.mu.Lock()
	e.expiresAt = time.Now().Add(-time.Second)
	e.mu.Unlock()

	if e.VerifyKey(st.Key) {
		t.Fatal("an EXPIRED window must admit nobody, even with the right key")
	}
	if e.Status().Open {
		t.Fatal("an expired window must report closed")
	}
}

func TestEnrollment_CloseIsImmediate(t *testing.T) {
	e := NewEnrollment(nil, nil, nil)
	st, _ := e.Open(t.Context(), time.Hour, 1)
	e.Close()
	if e.VerifyKey(st.Key) {
		t.Fatal("a closed window must admit nobody")
	}
}

// Re-opening replaces the window. There is only ever ONE key, so an admin cannot accidentally
// leave two valid.
func TestEnrollment_ReopenInvalidatesTheOldKey(t *testing.T) {
	e := NewEnrollment(nil, nil, nil)
	first, _ := e.Open(t.Context(), time.Hour, 1)
	second, _ := e.Open(t.Context(), time.Hour, 1)

	if e.VerifyKey(first.Key) {
		t.Fatal("re-opening must invalidate the previous key")
	}
	if !e.VerifyKey(second.Key) {
		t.Fatal("the new key must work")
	}
}

// A window measured in days is not a window. The TTL is clamped.
func TestEnrollment_TTLIsClamped(t *testing.T) {
	e := NewEnrollment(nil, nil, nil)
	st, _ := e.Open(t.Context(), 30*24*time.Hour, 1)
	if until := time.Until(time.Unix(st.ExpiresAt, 0)); until > time.Hour+time.Minute {
		t.Fatalf("a month-long window must be clamped, got %v", until)
	}
}

func TestPayloadKeys_ListsTopLevelFields(t *testing.T) {
	got := payloadKeys([]byte(`{"temperature":21.4,"humidity":55,"battery":92}`))
	want := []string{"battery", "humidity", "temperature"} // sorted
	if len(got) != 3 || got[0] != want[0] || got[2] != want[2] {
		t.Fatalf("expected %v, got %v", want, got)
	}
	if payloadKeys([]byte("not json")) != nil {
		t.Fatal("a non-JSON payload has no keys")
	}
}

func TestTopicPrefix(t *testing.T) {
	if got := topicPrefix("zigbee2mqtt/{deviceKey}"); got != "zigbee2mqtt/" {
		t.Fatalf("got %q", got)
	}
	if got := topicPrefix("{deviceKey}/status"); got != "" {
		t.Fatalf("a template with no fixed prefix has none, got %q", got)
	}
}
