package areas

// Area is one family of promises: its name, its one-line promise, the
// patterns the session gave for it and, once resolved, the files it holds.
type Area struct {
	Name    string
	Promise string
	Sources []string
	// Files are the slash paths the area holds, sorted. Empty until claimed.
	Files []string
}
