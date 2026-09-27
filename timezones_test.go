package main

import (
	"testing"
	"time"
)

func TestFriendlyTimeZonesLoadAndAreUnique(t *testing.T) {
	names := map[string]bool{}
	zones := map[string]bool{}
	for _, z := range friendlyTimeZones {
		if _, err := time.LoadLocation(z.Zone); err != nil {
			t.Errorf("%s: %v", z.Zone, err)
		}
		if names[z.Name] {
			t.Errorf("duplicate name %q", z.Name)
		}
		if zones[z.Zone] {
			t.Errorf("duplicate zone %q", z.Zone)
		}
		names[z.Name], zones[z.Zone] = true, true
	}
}

func TestTimeZoneOptionsFollowDST(t *testing.T) {
	for _, tc := range []struct {
		now  time.Time
		want string
	}{
		{time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC), "(GMT-08:00) Pacific Time (US & Canada)"},
		{time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC), "(GMT-07:00) Pacific Time (US & Canada)"},
	} {
		var got string
		for _, o := range timeZoneOptions("America/Los_Angeles", tc.now) {
			if o.Zone == "America/Los_Angeles" {
				if !o.Selected {
					t.Error("current zone not selected")
				}
				got = o.Label
			}
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.now.Month(), got, tc.want)
		}
	}
}

func TestTimeZoneOptionsSortedByOffset(t *testing.T) {
	opts := timeZoneOptions("UTC", time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC))
	for i := 1; i < len(opts); i++ {
		if opts[i-1].offset > opts[i].offset {
			t.Fatalf("%q before %q", opts[i-1].Label, opts[i].Label)
		}
	}
	if opts[0].Zone != "Etc/GMT+12" {
		t.Errorf("first option %q", opts[0].Zone)
	}
}

func TestTimeZoneOptionsKeepUnlistedCurrentZone(t *testing.T) {
	if listedTimeZone("America/Boise") {
		t.Fatal("test needs a zone that isn't listed")
	}
	opts := timeZoneOptions("America/Boise", time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC))
	if len(opts) != len(friendlyTimeZones)+1 {
		t.Fatalf("%d options", len(opts))
	}
	for _, o := range opts {
		if o.Zone == "America/Boise" {
			if !o.Selected || o.Label != "(GMT-07:00) Boise" {
				t.Errorf("got %+v", o)
			}
			return
		}
	}
	t.Error("current zone missing")
}

func TestTimeZoneLabel(t *testing.T) {
	for zone, want := range map[string]string{
		"America/Los_Angeles":            "Pacific Time (US & Canada)",
		"UTC":                            "UTC",
		"America/Argentina/Buenos_Aires": "Buenos Aires",
		"America/Port_of_Spain":          "Port of Spain",
	} {
		if got := timeZoneLabel(zone); got != want {
			t.Errorf("%s: got %q, want %q", zone, got, want)
		}
	}
}
