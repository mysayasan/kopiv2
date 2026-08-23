package mailer

import (
	"fmt"
	"strings"
)

// RejectedRecipient is one address the relay refused at RCPT time, with the
// relay's own reason so an operator can tell a typo from a full mailbox.
type RejectedRecipient struct {
	Address string
	Err     error
}

// RecipientError reports that a message was not delivered to every recipient.
//
// It is returned in TWO different situations that callers must tell apart:
//   - AllRejected: nobody received the message. The send failed.
//   - otherwise: the message WAS delivered, to the accepted recipients only.
//
// The distinction is what lets a delivery channel retry a total failure while
// treating a partial one as done — retrying a partial send would deliver the
// alert to the working addresses again on every attempt.
type RecipientError struct {
	Rejected    []RejectedRecipient
	AllRejected bool
}

func (e *RecipientError) Error() string {
	parts := make([]string, 0, len(e.Rejected))
	for _, r := range e.Rejected {
		parts = append(parts, fmt.Sprintf("%s (%v)", r.Address, r.Err))
	}
	if e.AllRejected {
		return "smtp: every recipient was rejected: " + strings.Join(parts, "; ")
	}
	return "smtp: delivered, but some recipients were rejected: " + strings.Join(parts, "; ")
}

// Addresses lists the rejected addresses, for logging without the reasons.
func (e *RecipientError) Addresses() []string {
	out := make([]string, 0, len(e.Rejected))
	for _, r := range e.Rejected {
		out = append(out, r.Address)
	}
	return out
}

// Delivered reports whether the message reached at least one recipient.
func (e *RecipientError) Delivered() bool { return e != nil && !e.AllRejected }
