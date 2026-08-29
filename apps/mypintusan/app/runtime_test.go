package app

import "testing"

// TestAnnounceBusDownAlarmsOncePerOutage guards the damping on the segment-down alarm.
//
// The alarm exists because an unplugged adapter used to raise nothing at all, and every door on
// the segment went out of service silently. The damping exists because the obvious fix — alarm
// whenever the bus session ends — turns a rebooting serial-to-Ethernet gateway into a page every
// second or two: it accepts the TCP connection and drops it again for as long as the reboot takes,
// and `superviseBus` re-dials each time. Delivering the alarm-fatigue failure `services/alarm.go`
// argues against, through the very alarm added to prevent silence, would be the worse defect.
func TestAnnounceBusDownAlarmsOncePerOutage(t *testing.T) {
	r := &runtime{busDown: map[string]bool{}}
	const port = "tcp://gateway:4870"

	if !r.announceBusDown(port, false) {
		t.Fatal("the first loss of a segment must alarm, healthy prior session or not")
	}
	// The gateway is rebooting: connection accepted, connection dropped, over and over.
	for i := 0; i < 5; i++ {
		if r.announceBusDown(port, false) {
			t.Fatalf("flap %d alarmed again inside the same outage", i+1)
		}
	}
	// It came back and ran healthily, and has now failed again. That is a NEW outage, and the
	// operator has to hear about it — a segment that has recovered once must not be permanently
	// muted by the alarm it raised the first time.
	if !r.announceBusDown(port, true) {
		t.Fatal("a segment that recovered and then failed again must alarm again")
	}
	if r.announceBusDown(port, false) {
		t.Fatal("the new outage alarmed twice")
	}

	// Segments are independent: a cable pulled on one must not silence the other.
	r2 := &runtime{busDown: map[string]bool{}}
	r2.announceBusDown("tcp://a:1", false)
	if !r2.announceBusDown("tcp://b:1", false) {
		t.Fatal("one segment's outage suppressed another segment's alarm")
	}
}
