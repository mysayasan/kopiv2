package services

import (
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// mymatasan's page catalog — the surfaces an administrator grants, and the API access each one
// needs to actually work.
//
// This is the human-facing half of authorization. Policy() below remains the complete governed
// API surface and is what the golden test in pages_test.go measures this against; the catalog
// here is how a person says "this role may watch live video and review footage, and nothing
// else" without knowing that PTZ lives at /api/cameras/*/ptz.
//
// Adding a page: declare it here with the paths its levels need, add the id to the SPA's nav
// map, and add it to any preset that should hold it. The tests will tell you if a level grants a
// path Policy() does not govern, or if a preset stops matching the role it replaces.

// Page ids. Stable — stored against roles and mapped to nav entries by the SPA.
const (
	PageDashboard     = "dashboard"
	PageLiveViews     = "live-views"
	PageCameras       = "cameras"
	PageDetection     = "detection"
	PageTeach         = "teach"
	PagePeople        = "people"
	PageObjects       = "objects"
	PageCases         = "cases"
	PageRecordings    = "recordings"
	PageNotifications = "notifications"
	PageSettings      = "settings"
	PageAudit         = "audit"
)

// Level ids. Cumulative, in this order, wherever a page declares more than one.
const (
	LevelView   = "view"
	LevelUse    = "use"
	LevelManage = "manage"
)

// Nav groups, matching the rail's own grouping.
const (
	groupWorkspace    = "workspace"
	groupSurveillance = "surveillance"
	groupIntelligence = "intelligence"
	groupSystem       = "system"
)

// Grant helpers, named so the catalog below reads as prose. They are distinct from rbac.go's
// read/write, which are Verbs values for the API-path catalog rather than path grants.
var (
	canRead  = sharedservices.Read
	canWrite = sharedservices.Write
	canAll   = sharedservices.Full
	// canUse is read plus post: the shape an area needs when its write is asynchronous
	// (start an export, poll it, download it) or when writing means reading back. It is
	// deliberately not Full — DELETE is the verb that destroys records, and no grantable
	// page level hands it out.
	canUse = func(path string) sharedservices.PathGrant {
		return sharedservices.PathGrant{Path: path, CanGet: true, CanPost: true}
	}
)

// Pages returns mymatasan's page catalog.
func Pages() sharedservices.PageCatalog {
	return sharedservices.PageCatalog{
		// Granted to every role regardless of pages: without these a role cannot complete the
		// sign-in sequence at all, so they must not be something an administrator can untick.
		// The appliance shipped once without the session grant and locked two of three roles
		// out of the product entirely — see the comment on PageCatalog.Baseline.
		Baseline: []sharedservices.PathGrant{
			canRead("/api/auth/session"),
			canWrite("/api/auth/change-password"),
			canRead("/api/setup"),
		},

		Pages: []sharedservices.Page{
			{
				Id: PageDashboard, Group: groupWorkspace, Order: 10,
				Levels: []sharedservices.PageLevel{{
					Id: LevelView,
					// The dashboard is built from the alert log, the notification feed and the
					// capacity estimate; it owns none of them.
					Grants: []sharedservices.PathGrant{
						canRead("/api/vision"), canRead("/api/notifications"), canRead("/api/capacity"),
					},
				}},
			},
			{
				Id: PageLiveViews, Group: groupWorkspace, Order: 20,
				Levels: []sharedservices.PageLevel{
					{
						Id: LevelView,
						Grants: []sharedservices.PathGrant{
							canRead("/api/cameras"),
							canWrite("/api/cameras/health/refresh"),
							canWrite("/api/cameras/*/live-view"),
							canWrite("/api/cameras/*/webrtc/offer"),
							// The named walls this page renders. Reading them is part of
							// watching: a viewer who cannot load the wall gets a blank grid
							// and no way to tell that from "no cameras".
							canRead("/api/walls"),
							// Runtime settings drive live view; the SPA loads them before it can
							// render the wall at all.
							canRead("/api/settings"),
						},
					},
					{
						// Moving a camera and talking through it change the physical world, so
						// they are a rung above watching it.
						Id: LevelUse,
						Grants: []sharedservices.PathGrant{
							canWrite("/api/cameras/*/ptz"), canWrite("/api/cameras/*/talk/offer"),
							// Arranging the walls, which is the same rung as moving a camera:
							// it changes what a room sees rather than merely watching it.
							canUse("/api/walls"),
						},
					},
				},
			},
			{
				Id: PageNotifications, Group: groupSystem, Order: 10,
				Levels: []sharedservices.PageLevel{
					{Id: LevelView, Grants: []sharedservices.PathGrant{canRead("/api/notifications"), canRead("/api/vision")}},
					{
						// Acknowledging is an act, not a read: it changes the work queue every
						// other operator is looking at.
						Id: LevelUse,
						Grants: []sharedservices.PathGrant{
							canWrite("/api/vision/alerts/*/ack"), canWrite("/api/notifications/*/read"),
						},
					},
				},
			},
			{
				Id: PageRecordings, Group: groupWorkspace, Order: 30,
				Levels: []sharedservices.PageLevel{
					{
						Id:     LevelView,
						Grants: []sharedservices.PathGrant{canRead("/api/recording")},
					},
					{
						// The second rung adds EXPORTING and nothing else. Deleting footage is
						// still superadmin-only and is deliberately not a level an administrator
						// can hand out: an operator who was present at an incident must be able
						// to hand the footage of it to somebody, and must not be able to destroy
						// it. Those are different capabilities and this is where they separate.
						//
						// Every export is audited with the operator's stated reason, which is
						// what makes granting this rung safe rather than merely convenient.
						// canUse, not canWrite: an export is built asynchronously, so the
						// exporter must be able to poll the job and download the bundle.
						// With POST alone the rung granted the right to start an export and
						// nothing else, which is not the capability it describes.
						Id:     LevelUse,
						Grants: []sharedservices.PathGrant{canUse("/api/evidence")},
					},
				},
			},
			{
				Id: PageObjects, Group: groupIntelligence, Order: 30,
				Levels: []sharedservices.PageLevel{{
					Id:     LevelView,
					Grants: []sharedservices.PathGrant{canRead("/api/observations")},
				}},
			},
			{
				// Cases sit in the workspace beside Recordings because they are the same
				// job: reviewing what happened. Reading one is not the same as working it,
				// so the second rung is where opening, annotating and closing live.
				Id: PageCases, Group: groupWorkspace, Order: 40,
				Levels: []sharedservices.PageLevel{
					{
						Id:     LevelView,
						Grants: []sharedservices.PathGrant{canRead("/api/cases")},
					},
					{
						// Exporting a case is an evidence export and is audited as one, so
						// this rung also needs what the Recordings rung needs. A role given
						// Cases-use without Recordings-use can still export its own cases —
						// that is the point of the grant living here too.
						Id:     LevelUse,
						Grants: []sharedservices.PathGrant{canUse("/api/cases")},
					},
				},
			},

			// --- Administrative surfaces -------------------------------------------------
			// Listed so the catalog is a complete description of the app, and marked AdminOnly
			// so they can never be granted. A page missing from this list is a page nobody can
			// see they are not handing out.
			{Id: PageCameras, Group: groupSurveillance, Order: 10, AdminOnly: true,
				Levels: []sharedservices.PageLevel{{Id: LevelManage, Grants: []sharedservices.PathGrant{canAll("/api/cameras"), canAll("/api/onvif")}}}},
			{Id: PageDetection, Group: groupSurveillance, Order: 20, AdminOnly: true,
				Levels: []sharedservices.PageLevel{{Id: LevelManage, Grants: []sharedservices.PathGrant{canAll("/api/vision"), canAll("/api/anomaly")}}}},
			{Id: PageTeach, Group: groupIntelligence, Order: 10, AdminOnly: true,
				Levels: []sharedservices.PageLevel{{Id: LevelManage, Grants: []sharedservices.PathGrant{canAll("/api/teach"), canAll("/api/training")}}}},
			{Id: PagePeople, Group: groupIntelligence, Order: 20, AdminOnly: true,
				Levels: []sharedservices.PageLevel{{Id: LevelManage, Grants: []sharedservices.PathGrant{canAll("/api/faces")}}}},
			{Id: PageSettings, Group: groupSystem, Order: 20, AdminOnly: true,
				Levels: []sharedservices.PageLevel{{Id: LevelManage, Grants: []sharedservices.PathGrant{
					canAll("/api/settings"), canAll("/api/system"), canAll("/api/pairing"),
				}}}},
			// The audit trail is its own page rather than a corner of Settings, because it is
			// what an auditor is sent to look at and it answers a different question from
			// every other screen: not "how is this configured" but "what did people do".
			// READ ONLY at every level — the API has no mutating route to grant.
			{Id: PageAudit, Group: groupSystem, Order: 30, AdminOnly: true,
				Levels: []sharedservices.PageLevel{{Id: LevelView, Grants: []sharedservices.PathGrant{
					canRead("/api/audit"),
				}}}},
		},

		// Paths that sit UNDER a path some page grants, and must be denied unless a page names
		// them. Without these rows the broader grant governs and the narrower area is wide open
		// — a role with the Live Views page would be able to read every user account, because
		// /api/settings is granted and nothing narrower exists to say otherwise.
		Carveouts: []string{
			"/api/settings/users",
			"/api/settings/roles",
		},
	}
}

// RolePresets maps the three built-in roles onto page grants.
//
// These must produce exactly the matrix Policy() produces today — pages_test.go asserts it, so
// adopting page-level access provably cannot widen a built-in role's reach. That equivalence is
// the only thing standing between this refactor and a silent authorization regression.
func RolePresets() map[string][]sharedservices.PageGrant {
	return map[string][]sharedservices.PageGrant{
		// Watches live video and sees that an alert fired. That is the whole role.
		RoleViewer: {
			{PageId: PageDashboard, Level: LevelView},
			{PageId: PageLiveViews, Level: LevelView},
			{PageId: PageNotifications, Level: LevelView},
		},
		// Everything a viewer can do, plus reviewing what actually happened and acting on it.
		RoleOperator: {
			{PageId: PageDashboard, Level: LevelView},
			{PageId: PageLiveViews, Level: LevelUse},
			{PageId: PageNotifications, Level: LevelUse},
			{PageId: PageRecordings, Level: LevelUse},
			{PageId: PageObjects, Level: LevelView},
			{PageId: PageCases, Level: LevelUse},
		},
	}
}
