package recoverinput

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/place"
)

// placeStore holds one stored placement.
type placeStore struct{ body []byte }

func (s placeStore) Place(ctx context.Context) (initrecord.Place, bool, error) {
	return initrecord.Place{Body: s.body}, s.body != nil, nil
}

func TestChooseKeepsOnlysOrderOnce(t *testing.T) {
	names := []string{"export", "search", "tags"}
	if got, err := Choose(names, nil, "%q %s"); err != nil || !slices.Equal(got, []int{0, 1, 2}) {
		t.Errorf("all: %v, %v", got, err)
	}
	if got, err := Choose(names, []string{"tags", "export", "tags"}, "%q %s"); err != nil || !slices.Equal(got, []int{2, 0}) {
		t.Errorf("only: %v, %v", got, err)
	}
	if _, err := Choose(names, []string{"billing"}, "no %q; there are %s"); err == nil || err.Error() != `no "billing"; there are export, search, tags` {
		t.Errorf("missing: %v", err)
	}
}

func TestChosenOrdersTheWalkBySeedOrByOnly(t *testing.T) {
	wb, err := json.Marshal(testWalk())
	if err != nil {
		t.Fatal(err)
	}
	pb, err := json.Marshal(place.Record{ByCapability: []place.CapabilityPlaced{{Capability: "search", PRs: []int{7}}}})
	if err != nil {
		t.Fatal(err)
	}
	w, g, root := initrecord.Walk{Body: wb}, testGather(), t.TempDir()
	got, err := Chosen(t.Context(), placeStore{pb}, root, w, g, nil, 42)
	if want := Order(Walk(root, testWalk(), place.Record{}, g), 42); err != nil || !slices.Equal(names(got), names(want)) {
		t.Errorf("all: %v, want %v (%v)", names(got), names(want), err)
	}
	got, err = Chosen(t.Context(), placeStore{pb}, root, w, g, []string{"search"}, 42)
	if err != nil || len(got) != 1 || len(got[0].Sources.PRs) != 1 || got[0].Sources.PRs[0].Number != 7 {
		t.Errorf("only search: %+v, %v", got, err)
	}
	if _, err := Chosen(t.Context(), placeStore{pb}, root, w, g, []string{"billing"}, 42); err == nil || !strings.Contains(err.Error(), `no capability "billing" in the walk; it found: export, search`) {
		t.Errorf("unknown: %v", err)
	}
	if _, err := Chosen(t.Context(), placeStore{pb}, root, initrecord.Walk{Body: []byte(`{"capabilities":[]}`)}, g, nil, 42); err == nil {
		t.Error("an empty walk gave no error")
	}
}
