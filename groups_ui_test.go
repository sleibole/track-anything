package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestOrganizeBuildsAndClearsAGroup(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	h := ts.personalHousehold(t, "owner@example.com")
	ate := owner.createTracker(h, "Millie Ate", nil)
	woods := owner.createTracker(h, "Woods Run", nil)
	park := owner.createTracker(h, "Dog Park", nil)

	home := owner.get("/").body
	if !strings.Contains(home, "Organize") || !strings.Contains(home, "New tracker") {
		t.Fatal("home is missing Organize or New tracker")
	}
	if strings.Contains(home, `action="/groups"`) {
		t.Fatal("home still has the inline create-group form")
	}

	org := owner.get("/organize")
	if org.status != http.StatusOK || strings.Contains(org.body, `id="tracker-refresh"`) {
		t.Fatalf("organize page: %d refresh=%v", org.status, strings.Contains(org.body, `id="tracker-refresh"`))
	}
	for _, name := range []string{"Millie Ate", "Woods Run", "Dog Park", "Move to group", "New group", "None (ungrouped)", "Done"} {
		if !strings.Contains(org.body, name) {
			t.Errorf("organize missing %q", name)
		}
	}
	if strings.Contains(org.body, "data-occurrence") || strings.Contains(org.body, "+ Log") {
		t.Fatal("organize still shows logging controls")
	}

	form := url.Values{}
	form.Add("id", itoa(ate.ID))
	form.Add("id", itoa(woods.ID))
	form.Add("id", itoa(park.ID))
	form.Set("group", "new")
	form.Set("group_name", "Millie")
	form.Set("next", "/organize")
	if r := owner.post("/organize/move", form); r.url.Path != "/organize" || !strings.Contains(r.body, "Created the group.") {
		t.Fatalf("create from selection: %d %s\n%s", r.status, r.url, r.body)
	}
	list := owner.get("/").body
	assertOrder(t, list, "Millie", "Millie Ate", "Woods Run", "Dog Park")
	if strings.Contains(list, "Ungrouped") {
		t.Fatal("home invented an Ungrouped heading")
	}
	millie := ts.groupNamed(t, "owner@example.com", "Millie")
	if millie.Collapsed {
		t.Fatal("new group started collapsed")
	}

	if r := owner.post("/groups/"+itoa(millie.ID)+"/collapse", url.Values{"collapsed": {"1"}}); r.url.Path != "/" {
		t.Fatalf("collapse: %d %s", r.status, r.url)
	}
	collapsed := owner.get("/").body
	if !groupBodyHidden(collapsed, millie.ID) || !strings.Contains(collapsed, `aria-expanded="false"`) {
		t.Fatal("collapse did not persist on reload")
	}
	if !strings.Contains(collapsed, "Millie Ate") {
		t.Fatal("collapsed group dropped its trackers from the page")
	}

	out := url.Values{}
	out.Add("id", itoa(park.ID))
	out.Set("group", "")
	if r := owner.post("/organize/move", out); !strings.Contains(r.body, "out of their groups") {
		t.Fatalf("ungroup: %d\n%s", r.status, r.body)
	}
	after := owner.get("/").body
	assertOrder(t, after, "Millie", "Millie Ate", "Woods Run", "Dog Park")

	if r := owner.post("/groups/"+itoa(millie.ID), url.Values{"name": {"Mornings"}}); !strings.Contains(owner.get("/").body, "Mornings") || strings.Contains(owner.get("/").body, ">Millie<") {
		t.Fatalf("rename: %d", r.status)
	}
	if r := owner.post("/groups/"+itoa(millie.ID)+"/delete", nil); r.url.Path != "/" {
		t.Fatalf("delete: %d %s", r.status, r.url)
	}
	gone := owner.get("/").body
	if strings.Contains(gone, "Mornings") || strings.Contains(gone, "data-group") {
		t.Fatal("deleted group is still on the list")
	}
	for _, name := range []string{"Millie Ate", "Woods Run", "Dog Park"} {
		if !strings.Contains(gone, name) {
			t.Errorf("deleted group removed %s", name)
		}
	}
	if len(ts.entries(t, ate.ID)) != 0 {
		t.Fatal("organize changed tracker entries")
	}
}

func TestOrganizeRejectsAForeignTracker(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	h := ts.personalHousehold(t, "owner@example.com")
	mine := owner.createTracker(h, "Mine", nil)
	other := ts.browser(t)
	other.signup("other@example.com", "")
	hidden := other.createTracker(ts.personalHousehold(t, "other@example.com"), "Hidden", nil)

	form := url.Values{}
	form.Add("id", itoa(mine.ID))
	form.Add("id", itoa(hidden.ID))
	form.Set("group", "new")
	form.Set("group_name", "Nope")
	if r := owner.post("/organize/move", form); r.status != http.StatusNotFound {
		t.Fatalf("foreign tracker: %d", r.status)
	}
	var n int64
	if err := ts.app.db.Model(&TrackerGroup{}).Where("name = ?", "Nope").Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("rejected move created %d groups (%v)", n, err)
	}
}
