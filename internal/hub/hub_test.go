package hub

import "testing"

func TestByStationID(t *testing.T) {
	cases := []struct {
		stationID int64
		want      string
		wantOK    bool
	}{
		{60003760, "Jita", true},
		{60004588, "Rens", true},
		{60012345, "", false},
	}
	for _, c := range cases {
		h, ok := ByStationID(c.stationID)
		if ok != c.wantOK {
			t.Errorf("ByStationID(%d) ok = %v, want %v", c.stationID, ok, c.wantOK)
			continue
		}
		if ok && h.Name != c.want {
			t.Errorf("ByStationID(%d).Name = %q, want %q", c.stationID, h.Name, c.want)
		}
	}
}

func TestByName(t *testing.T) {
	h, ok := ByName("Jita")
	if !ok || h.RegionID != 10000002 {
		t.Errorf("ByName(Jita) = %+v, ok=%v, want RegionID 10000002", h, ok)
	}

	if _, ok := ByName("Amarr"); ok {
		t.Error("ByName(Amarr) ok = true, want false (not one of the two supported Hubs)")
	}
}

func TestAllContainsBothHubs(t *testing.T) {
	if len(All) != 2 {
		t.Fatalf("len(All) = %d, want 2", len(All))
	}
	names := map[string]bool{All[0].Name: true, All[1].Name: true}
	if !names["Jita"] || !names["Rens"] {
		t.Errorf("All = %+v, want Jita and Rens", All)
	}
}
