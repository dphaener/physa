package store

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// WriteRecords writes slice records, newest first as given: one line each for
// people, or one JSON list for tools ([] when there are none).
func WriteRecords(w io.Writer, recs []SliceRecord, asJSON bool) error {
	if asJSON {
		if recs == nil {
			recs = []SliceRecord{}
		}
		return json.NewEncoder(w).Encode(recs)
	}
	if len(recs) == 0 {
		_, err := fmt.Fprintln(w, "No slice records.")
		return err
	}
	for _, r := range recs {
		session := r.Session
		if session == "" {
			session = "—"
		}
		line := fmt.Sprintf("%s %s %s", r.At.UTC().Format(time.RFC3339), session, strings.Join(r.Capabilities, ","))
		if r.With != "" {
			line += " with " + r.With
		}
		if r.Budget != 0 {
			line += " budget " + strconv.Itoa(r.Budget)
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}
