// Package schemas embeds the published JSON Schema documents of this
// directory (docs/ARRANQUE.md §6.2) so the binary serves them without the
// repository: `trafico schema <nombre>` reads them from here (go:embed),
// never from disk, because the binary installed by a release ships without
// the repository beside it. The files of this directory remain the single
// source of the contract.
package schemas

import "embed"

// FS holds every published schema document, byte for byte the files of
// this directory.
//
//go:embed *.json
var FS embed.FS
