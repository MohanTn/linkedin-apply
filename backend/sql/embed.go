// Package sqlddl embeds the canonical DDL so migrations run from a single source.
package sqlddl

import _ "embed"

//go:embed schema.sql
var Schema string
