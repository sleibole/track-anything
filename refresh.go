package main

import (
	"hash/fnv"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Tracker pages refresh by asking for this id. The response is a fragment of
// out-of-band swaps, or 204 when nothing on the page changed. It is not a new
// document and it does not replace #content.
const trackerRefreshID = "tracker-refresh"

func isTrackerRefresh(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Trigger") == trackerRefreshID
}

func revHash(parts ...string) string {
	h := fnv.New64a()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return strconv.FormatUint(h.Sum64(), 36)
}

func stampCard(c *trackerCard) {
	data := cardDataParts(*c)
	c.DataRev = revHash(data...)
	c.ViewRev = revHash(append(append([]string{}, data...), "undo:"+c.UndoURL)...)
}

// cardDataParts is the displayed card, without the undo control. The undo
// window expiring is not a data change, so it must not highlight the tracker.
// The summary text is included, so a new day changes the rev without a new entry.
func cardDataParts(c trackerCard) []string {
	t := c.Tracker
	return []string{
		"card",
		t.Name,
		t.Icon,
		t.Accent,
		t.Kind,
		t.Unit,
		t.Label(),
		t.AddLabel(),
		c.HouseholdLabel,
		c.Summary,
		strconv.Itoa(c.Count),
		c.Carry,
		c.CarryValue,
		strconv.FormatBool(c.LogAgain),
		strconv.FormatBool(c.NumberField),
		strconv.FormatBool(c.GoalProgress),
		c.ProgressValue,
		c.ProgressMax,
		c.GoalsCompleted,
		c.TotalEarned,
		c.Rewards,
	}
}

func entryDataParts(e entryView) []string {
	return []string{
		"entry",
		strconv.FormatUint(uint64(e.ID), 10),
		e.Time,
		e.Value,
		e.Note,
		e.Running,
		e.By,
	}
}

func todayDataParts(entries []entryView, today todayState) []string {
	zero := ""
	if today.Zero != nil {
		zero = strconv.FormatUint(uint64(today.Zero.ID), 10)
	}
	parts := []string{"today", strconv.Itoa(today.Count), zero}
	for _, e := range entries {
		parts = append(parts, entryDataParts(e)...)
	}
	return parts
}

func undoExtras(undo string, zeroNew bool, entries []entryView) []string {
	parts := []string{"undo:" + undo, "zeroNew:" + strconv.FormatBool(zeroNew)}
	for _, e := range entries {
		parts = append(parts, "eundo:"+strconv.FormatUint(uint64(e.ID), 10)+":"+e.UndoURL)
	}
	return parts
}

func stampTrackerRegions(p *trackerPage) {
	stampCard(&p.Card)
	logData := append(cardDataParts(p.Card), todayDataParts(p.Entries, p.Today)...)
	logData = append(logData, rewardRegionParts(p)...)
	p.LogDataRev = revHash(logData...)
	p.LogViewRev = revHash(append(append([]string{}, logData...), undoExtras(p.Card.UndoURL, p.ZeroNew, p.Entries)...)...)
	hist := historyDataParts(p.History, p.PeriodNote)
	p.HistoryDataRev = revHash(hist...)
	p.HistoryViewRev = revHash(append(append([]string{}, hist...), historyViewExtras(p.History)...)...)
	trend := trendParts(p)
	p.TrendDataRev = revHash(trend...)
	p.TrendViewRev = p.TrendDataRev
}

func rewardRegionParts(p *trackerPage) []string {
	if !p.Tracker().IsCumulative() {
		return nil
	}
	parts := []string{"rewards", p.RewardGap, p.RewardWindowNote}
	for _, v := range p.Redemptions {
		parts = append(parts, strconv.FormatUint(uint64(v.ID), 10), v.When, v.Note, v.By)
	}
	return parts
}

func historyDataParts(days []historyDay, period string) []string {
	parts := []string{"history", period}
	for _, d := range days {
		zero := ""
		if d.Zero != nil {
			zero = strconv.FormatUint(uint64(d.Zero.ID), 10)
		}
		parts = append(parts, d.Day, d.Label, d.Summary(), strconv.FormatBool(d.Logged()), zero)
		for _, e := range d.Entries {
			parts = append(parts, entryDataParts(e)...)
		}
	}
	return parts
}

func historyViewExtras(days []historyDay) []string {
	parts := []string{"history-undo"}
	for _, d := range days {
		parts = append(parts, d.Day+":"+strconv.FormatBool(d.ZeroNew))
		for _, e := range d.Entries {
			parts = append(parts, "eundo:"+strconv.FormatUint(uint64(e.ID), 10)+":"+e.UndoURL)
		}
	}
	return parts
}

func trendParts(p *trackerPage) []string {
	parts := []string{
		"trend",
		string(p.ChartScript),
		string(p.OverlayScript),
		strconv.FormatBool(p.OverlayEmpty),
		strconv.FormatBool(p.ChartScript != ""),
	}
	if p.Overlay != nil {
		parts = append(parts, p.Overlay.Name, p.Overlay.Icon)
	}
	for _, c := range p.OverlayChoices {
		parts = append(parts, strconv.FormatUint(uint64(c.ID), 10), c.Name, c.Icon)
	}
	return parts
}

func stampShare(p *sharePage) {
	stampCard(&p.Card)
	logData := append(cardDataParts(p.Card), todayDataParts(p.Entries, p.Today)...)
	p.LogDataRev = revHash(logData...)
	p.LogViewRev = revHash(append(append([]string{}, logData...), undoExtras(p.Card.UndoURL, p.ZeroNew, p.Entries)...)...)
}

func fillMarkers(p *dashboardPage) {
	ids := make([]string, len(p.Cards))
	for i, c := range p.Cards {
		ids[i] = strconv.FormatUint(uint64(c.Tracker.ID), 10)
	}
	p.Order = strings.Join(ids, ",")
	p.OrderRev = revHash("order", p.Order)
	p.ShowEmpty = len(p.Cards) == 0
	p.EmptyRev = revHash("empty", strconv.FormatBool(p.ShowEmpty))
}

func trackerDOMID(id uint) string {
	return "tracker-" + strconv.FormatUint(uint64(id), 10)
}

func logRegionID(id uint) string {
	return "tracker-log-" + strconv.FormatUint(uint64(id), 10)
}

func trendRegionID(id uint) string {
	return "tracker-trend-" + strconv.FormatUint(uint64(id), 10)
}

func historyRegionID(id uint) string {
	return "tracker-history-" + strconv.FormatUint(uint64(id), 10)
}

func isTrackerDOMID(id string) bool {
	rest, ok := strings.CutPrefix(id, "tracker-")
	if !ok || rest == "" {
		return false
	}
	for _, c := range rest {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// parsePresent reads the ids and view revs the page already has.
// The poller sends them as parallel comma-separated lists.
func parsePresent(r *http.Request) map[string]string {
	ids := splitList(r.URL.Query().Get("present"))
	revs := splitList(r.URL.Query().Get("rev"))
	out := make(map[string]string, len(ids))
	for i, id := range ids {
		rev := ""
		if i < len(revs) {
			rev = revs[i]
		}
		out[id] = rev
	}
	return out
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func writeNoChange(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) writeHomeRefresh(w http.ResponseWriter, r *http.Request, cards []trackerCard) {
	if _, ok := r.URL.Query()["present"]; !ok {
		writeNoChange(w)
		return
	}
	have := parsePresent(r)
	page := dashboardPage{Cards: cards}
	fillMarkers(&page)
	seen := make(map[string]bool, len(cards))
	changed := make([]trackerCard, 0)
	for i := range cards {
		id := trackerDOMID(cards[i].Tracker.ID)
		seen[id] = true
		prev, ok := have[id]
		if ok && prev == cards[i].ViewRev {
			continue
		}
		if ok {
			cards[i].OOB = "outerHTML"
		} else {
			cards[i].OOB = "beforeend:#tracker-list"
		}
		changed = append(changed, cards[i])
	}
	var removed []string
	for id := range have {
		if !seen[id] && isTrackerDOMID(id) {
			removed = append(removed, id)
		}
	}
	sort.Strings(removed)
	page.Cards = changed
	page.Removed = removed
	page.SwapOrder = have["tracker-order"] != page.OrderRev
	page.SwapEmpty = have["tracker-empty"] != page.EmptyRev
	page.OOB = true
	if len(changed) == 0 && len(removed) == 0 && !page.SwapOrder && !page.SwapEmpty {
		writeNoChange(w)
		return
	}
	a.renderBlock(w, r, http.StatusOK, "dashboard.html", "homeRefresh", page)
}

func (a *app) writeTrackerRefresh(w http.ResponseWriter, r *http.Request, p trackerPage) {
	if _, ok := r.URL.Query()["present"]; !ok {
		writeNoChange(w)
		return
	}
	have := parsePresent(r)
	id := p.Tracker().ID
	p.OOBLog = viewChanged(have, logRegionID(id), p.LogViewRev)
	p.OOBTrend = viewChanged(have, trendRegionID(id), p.TrendViewRev)
	p.OOBHistory = viewChanged(have, historyRegionID(id), p.HistoryViewRev)
	if !p.OOBLog && !p.OOBTrend && !p.OOBHistory {
		writeNoChange(w)
		return
	}
	a.renderBlock(w, r, http.StatusOK, "tracker_show.html", "trackerRefresh", p)
}

func (a *app) writeShareRefresh(w http.ResponseWriter, r *http.Request, p sharePage) {
	if _, ok := r.URL.Query()["present"]; !ok {
		writeNoChange(w)
		return
	}
	have := parsePresent(r)
	if !viewChanged(have, logRegionID(p.Tracker().ID), p.LogViewRev) {
		writeNoChange(w)
		return
	}
	p.OOBLog = true
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex")
	a.renderBlock(w, r, http.StatusOK, "share.html", "shareRefresh", p)
}

func viewChanged(have map[string]string, id, rev string) bool {
	prev, ok := have[id]
	return ok && prev != rev
}

// returnToPage sends a tracker write back to the page it came from.
// A normal submit is a 303. An HTMX submit swaps that tracker in place.
func (a *app) returnToPage(w http.ResponseWriter, r *http.Request, fallback string, trackerID uint, notice string) {
	if r.Header.Get("HX-Request") != "true" {
		to := redirectTarget(r, fallback)
		if notice != "" {
			to = withNotice(to, trackerID, notice)
		}
		http.Redirect(w, r, to, http.StatusSeeOther)
		return
	}
	if err := a.writeInPlace(w, r, trackerID, notice, fallback); err != nil {
		a.serverError(w, r, err)
	}
}

func (a *app) writeInPlace(w http.ResponseWriter, r *http.Request, trackerID uint, notice, fallback string) error {
	switch path := pagePath(r); {
	case path == "/":
		return a.writeHomeCard(w, r, trackerID, notice)
	case strings.HasPrefix(path, "/s/"):
		return a.writeShareUpdate(w, r, notice)
	case trackerShowPath(path):
		return a.writeTrackerUpdate(w, r, path, notice)
	default:
		to := redirectTarget(r, fallback)
		if notice != "" {
			to = withNotice(to, trackerID, notice)
		}
		redirectTo(w, r, to)
		return nil
	}
}

func pagePath(r *http.Request) string {
	raw := r.Header.Get("HX-Current-URL")
	if raw == "" {
		raw = r.Referer()
	}
	if raw == "" {
		if back := r.PostFormValue("back"); back != "" {
			raw = safeNext(back)
		}
	}
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}

func trackerShowPath(path string) bool {
	rest, ok := strings.CutPrefix(path, "/trackers/")
	if !ok || rest == "" || strings.Contains(rest, "/") {
		return false
	}
	return parseID(rest) != 0
}

func (a *app) writeHomeCard(w http.ResponseWriter, r *http.Request, trackerID uint, notice string) error {
	u := currentUser(r)
	if u == nil {
		redirectTo(w, r, "/login")
		return nil
	}
	cards, err := a.dashboard(u)
	if err != nil {
		return err
	}
	page := dashboardPage{}
	for i := range cards {
		if cards[i].Tracker.ID != trackerID {
			continue
		}
		if notice != "" {
			cards[i].Notice = notice
		}
		cards[i].OOB = "outerHTML"
		page.Cards = []trackerCard{cards[i]}
		w.Header().Set("HX-Reswap", "none")
		a.renderBlock(w, r, http.StatusOK, "dashboard.html", "homeRefresh", page)
		return nil
	}
	page.Removed = []string{trackerDOMID(trackerID)}
	w.Header().Set("HX-Reswap", "none")
	a.renderBlock(w, r, http.StatusOK, "dashboard.html", "homeRefresh", page)
	return nil
}

func (a *app) writeTrackerUpdate(w http.ResponseWriter, r *http.Request, path, notice string) error {
	u := currentUser(r)
	if u == nil {
		redirectTo(w, r, "/login")
		return nil
	}
	t, role, err := trackerForUser(a.db, u.ID, parseID(strings.TrimPrefix(path, "/trackers/")))
	if err != nil {
		a.lookupError(w, r, err)
		return nil
	}
	overlay, err := a.resolveOverlay(u.ID, t, requestedOverlayID(r))
	if err != nil {
		a.lookupError(w, r, err)
		return nil
	}
	p, err := a.trackerPage(t, role, u, overlay)
	if err != nil {
		return err
	}
	if notice != "" {
		p.Card.Notice = notice
	}
	p.OOBLog = true
	p.OOBTrend = true
	p.OOBHistory = true
	w.Header().Set("HX-Reswap", "none")
	a.renderBlock(w, r, http.StatusOK, "tracker_show.html", "trackerRefresh", p)
	return nil
}

func (a *app) writeShareUpdate(w http.ResponseWriter, r *http.Request, notice string) error {
	t, loc, ok := a.sharedTracker(w, r)
	if !ok {
		return nil
	}
	p, err := a.buildShare(t, loc, "", false, r)
	if err != nil {
		return err
	}
	if notice != "" {
		p.Card.Notice = notice
	}
	p.OOBLog = true
	w.Header().Set("HX-Reswap", "none")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex")
	a.renderBlock(w, r, http.StatusOK, "share.html", "shareRefresh", p)
	return nil
}
