package web

import "embed"

// FS embeds the static UI assets in this directory. It lists the files
// explicitly (rather than all:*) so this Go source is not itself served, and
// so adding a stray file to web/ can't silently leak. It is colocated here
// rather than referenced via "../../web" from internal/server because Go's
// //go:embed directive does not allow ".." in patterns: the source file must
// live within (or above) the directory tree it embeds. Extend the list when
// new asset files are added.
//
//go:embed index.html style.css app.js chart.js
var FS embed.FS
