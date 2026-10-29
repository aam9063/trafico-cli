package window

// The full package documentation lives on window.go, next to the code it
// describes. This file only exists so the package has a stable place for it if
// it ever needs splitting; an empty file with a second package comment would
// make `go doc` print the thinner summary first.
//
// Accents are matched as typed: "mié" and "mie" both work, but an
// NFD-decomposed accent (a base letter plus a combining mark, common when a
// name is pasted from macOS) is rejected with a usage error naming the token.
// Normalising Unicode would mean a new dependency for a case a user can fix by
// typing the day.
