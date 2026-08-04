package services

import "github.com/mysayasan/kopiv2/apps/mypintusan/entities"

// Entities is the schema this app owns, in dependency order for a human reading it rather than for
// the migrator (which does not care).
//
// It lives beside the store rather than in the app package deliberately: the store is what breaks
// if a table is missing, so keeping the list next to it means the two cannot drift. The composition
// root appends this to the shared appliance tables.
func Entities() []any {
	return []any{
		// People and what they carry.
		entities.Holder{},
		entities.Credential{},

		// The physical estate.
		entities.Door{},
		entities.Reader{},
		entities.ReaderProfile{},

		// The rules about who gets in.
		entities.AccessGroup{},
		entities.AccessGroupMember{},
		entities.Schedule{},
		entities.ScheduleWindow{},
		entities.Holiday{},
		entities.Grant{},

		// The append-only record of what actually happened.
		entities.AccessEvent{},
	}
}
