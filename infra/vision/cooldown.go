package vision

// Rule cooldown state lives in-process (a per-camera lastTriggered map) and starts empty
// on every boot. That is fine while the process stays up, and wrong the moment it
// doesn't: with an empty map, every rule's cooldown reads as zero, so a busy scene
// re-fires as soon as minFrames is met — and again after the next restart. A crash-restart
// loop turns a 30-minute cooldown into an alert storm, a notification storm, and (because
// each alert extracts a clip) an ffmpeg storm on top.
//
// DetectionRule.LastTriggeredAt is loaded from the database and carried all the way into
// the detector already. It was simply never read. seedCooldown reads it.

// seedCooldown carries a rule's persisted last-trigger time into the in-process cooldown
// map the first time this process sees the rule, so a restart does not reset its
// cooldown. Once the rule fires in-process the live value wins.
func seedCooldown(lastTriggered map[int64]int64, rule DetectionRule) {
	if rule.LastTriggeredAt <= 0 {
		return
	}
	if _, seen := lastTriggered[rule.Id]; seen {
		return
	}
	lastTriggered[rule.Id] = rule.LastTriggeredAt
}

// cooldownActive reports whether rule is still inside its cooldown window, seeding the
// persisted last-trigger time first. Use it for every cooldown check so no detector can
// forget to seed.
func cooldownActive(lastTriggered map[int64]int64, rule DetectionRule, now int64, cooldown int) bool {
	seedCooldown(lastTriggered, rule)
	last := lastTriggered[rule.Id]
	return last > 0 && now-last < int64(cooldown)
}
