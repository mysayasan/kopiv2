package entities

import sharedentities "github.com/mysayasan/kopiv2/domain/entities"

// RuntimeSetting is the shared persistent key-value row (domain/entities.RuntimeSetting),
// aliased so mymatasan's call sites keep compiling unchanged.
//
// As with LocalUser, this MUST stay an alias of a type still NAMED RuntimeSetting: the
// code-first bootstrap derives the table name by reflecting the struct name, so aliasing a
// differently-named type would rename runtime_setting out from under every deployed appliance
// — taking the fleet key, the pairing enrollment and every persisted setting with it.
type RuntimeSetting = sharedentities.RuntimeSetting
