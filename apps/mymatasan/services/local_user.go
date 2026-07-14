package services

import (
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// The appliance user/login model now lives in domain/shared/services so mymatasan and
// myiotsan run the SAME security-critical code — bcrypt handling, session comparison, the
// bcrypt verification cache, the last-admin guard — rather than two copies that drift.
// These aliases keep mymatasan's existing call sites unchanged.

type (
	// AuthenticatedUser is the principal a request is decided against.
	AuthenticatedUser = sharedservices.AuthenticatedUser
	// ILocalUserService is the appliance user store.
	ILocalUserService = sharedservices.ILocalUserService
	// AdminSeedResult reports what happened when the bootstrap admin was established.
	AdminSeedResult = sharedservices.AdminSeedResult

	CreateLocalUserRequest         = sharedservices.CreateLocalUserRequest
	UpdateLocalUserRequest         = sharedservices.UpdateLocalUserRequest
	ChangeLocalUserPasswordRequest = sharedservices.ChangeLocalUserPasswordRequest
	ResetLocalUserPasswordRequest  = sharedservices.ResetLocalUserPasswordRequest
)

// NewLocalUserService builds mymatasan's user store on the shared appliance implementation.
var NewLocalUserService = sharedservices.NewLocalUserService

var (
	// ErrLocalUserInvalidCredential is returned for a bad username or password.
	ErrLocalUserInvalidCredential = sharedservices.ErrLocalUserInvalidCredential
	// ErrLocalUserInactive is returned when the account exists but is disabled.
	ErrLocalUserInactive = sharedservices.ErrLocalUserInactive
)
