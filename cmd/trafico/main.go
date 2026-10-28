// Command trafico is the entry point of the trafico-cli binary.
package main

import (
	"os"
	// Embeds the IANA time zone database in the binary. Without it,
	// time.LoadLocation("Europe/Madrid") fails on a machine with no zoneinfo
	// (a bare Windows container, for one), and every --from/--to window would
	// silently fall back to UTC: a Madrid deadline would move by an hour or two
	// and a weekend query would drop or gain incidents at the edges.
	_ "time/tzdata"

	"github.com/aam9063/trafico-cli/internal/cli"
)

func main() {
	os.Exit(int(cli.Execute()))
}
