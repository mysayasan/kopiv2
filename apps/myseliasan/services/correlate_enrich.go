package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
)

// The fleet-rule enricher: appends recurrence context to a fired rule's
// notification, so the 03:00 alert reads "…— and this is the 3rd time this
// week", which changes how fast an operator moves.
//
// STRICTLY DETERMINISTIC. This runs inside the alert path (under the
// correlator's enrichTimeout), so it is a couple of bounded feed queries and
// string formatting — never a model call. The digest is where language lives;
// an alert is where facts live.

// enrichLookback is how far back recurrence is counted.
const enrichLookback = 7 * 24 * time.Hour

// enrichFetchCap bounds the rows examined per enrichment.
const enrichFetchCap = 1000

// ruleHistorySource is the sliver of the notification service the enricher reads.
type ruleHistorySource interface {
	List(ctx context.Context, limit, offset uint64, cameraId int64, unreadOnly bool, category, source string) ([]*sharedentities.Notification, uint64, error)
}

// NewFleetRuleEnricher returns the correlator enricher: it counts this rule's
// firings over the trailing week and phrases the recurrence. Empty string when
// this is the first firing (a first alert needs no history) or on any error.
func NewFleetRuleEnricher(notif ruleHistorySource) func(ctx context.Context, ruleName string) string {
	return func(ctx context.Context, ruleName string) string {
		from := time.Now().Add(-enrichLookback).Unix()
		var (
			count  int
			lastAt int64
		)
		const page = 200
		for offset := uint64(0); offset < enrichFetchCap; offset += page {
			rows, _, err := notif.List(ctx, page, offset, 0, false, "", "fleet-rule")
			if err != nil || len(rows) == 0 {
				break
			}
			done := false
			for _, n := range rows {
				if n.CreatedAt < from {
					done = true
					break
				}
				if strings.EqualFold(n.Title, ruleName) {
					count++
					if n.CreatedAt > lastAt {
						lastAt = n.CreatedAt
					}
				}
			}
			if done || len(rows) < page {
				break
			}
		}
		if count == 0 {
			return ""
		}
		// count is PRIOR firings (this one has not been published yet).
		return fmt.Sprintf("Context: this rule also fired %d time(s) in the last 7 days, most recently %s.",
			count, time.Unix(lastAt, 0).Format("2006-01-02 15:04"))
	}
}
