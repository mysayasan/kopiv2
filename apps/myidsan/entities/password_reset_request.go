package entities

// PasswordResetRequest is one account-recovery request raised from the public
// "forgot password" form. It is the always-on, air-gap-safe recovery channel: a
// superadmin sees the pending queue and resolves each one by issuing a temporary
// password to hand over out-of-band. When the optional SMTP self-service path is
// configured, the user can instead complete the reset themselves and the row is
// marked resolved with Channel="self".
//
// A row is created ONLY when the submitted identifier matches an existing LOCAL
// account — but the public endpoint always returns the same generic response, so
// the row's existence is never observable to the requester (no account-enumeration
// oracle).
type PasswordResetRequest struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	UserLoginId int64  `json:"userLoginId" form:"userLoginId" query:"userLoginId" validate:"required"`
	// Email is the resolved account email, shown to the operator in the queue.
	Email string `json:"email" form:"email" query:"email"`
	// Status is "pending" until an operator resolves or dismisses it.
	Status string `json:"status" form:"status" query:"status"`
	// Channel records how it was resolved: "admin" (operator issued a temp password)
	// or "self" (user completed the SMTP self-service link). Empty while pending.
	Channel string `json:"channel" form:"channel" query:"channel"`
	// RequestIp is the connecting peer at request time — operator abuse context.
	RequestIp   string `json:"requestIp" form:"requestIp" query:"requestIp"`
	RequestedAt int64  `json:"requestedAt" form:"requestedAt" query:"requestedAt"`
	ResolvedBy  int64  `json:"resolvedBy" form:"resolvedBy" query:"resolvedBy"`
	ResolvedAt  int64  `json:"resolvedAt" form:"resolvedAt" query:"resolvedAt"`
}
