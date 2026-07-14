package entities

import sharedentities "github.com/mysayasan/kopiv2/domain/entities"

// LocalUser is the shared appliance user (domain/entities.LocalUser), aliased so
// mymatasan's existing call sites keep compiling unchanged.
//
// It MUST stay an alias of a type still NAMED LocalUser: the code-first bootstrap derives
// the table name by reflecting the struct name (strcase.ToSnake(typeOf.Name())), so
// aliasing a type named anything else would rename local_user out from under every
// deployed appliance.
type LocalUser = sharedentities.LocalUser
