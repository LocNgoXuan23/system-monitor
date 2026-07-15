package web

import "embed"

// FS embeds the static UI assets in this directory (recursively, including
// future subdirectories added by Tasks 13-14). It is colocated here rather
// than referenced via "../../web" from internal/server because Go's
// //go:embed directive does not allow ".." in patterns: the source file
// must live within (or above) the directory tree it embeds.
//
//go:embed all:*
var FS embed.FS
