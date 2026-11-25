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
	"github.com/aam9063/trafico-cli/internal/tui"
)

func main() {
	// No arguments and a terminal on both ends: open the interface. Anything
	// else -- a flag, a pipe, CI, an agent -- runs the CLI, which is the
	// contract. Drawing escape codes into a log file is worse than no UI.
	if len(os.Args) == 1 && isTerminal(os.Stdin) && isTerminal(os.Stdout) {
		cfg, fetcher, err := cli.LoadFeedsContext()
		if err == nil {
			if err := tui.Run(cfg, fetcher); err != nil {
				os.Exit(int(cli.Execute()))
			}
			return
		}
	}
	os.Exit(int(cli.Execute()))
}

// isTerminal reports whether the file is a character device, which is what a
// terminal is. It needs no dependency and behaves the same on the three
// release targets.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
