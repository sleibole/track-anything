package main

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// exportHeader is the CSV for one tracker. occurred_at is UTC.
// day is the exporter's local calendar day. duration_seconds is the stored length.
// duration is the same length in words. A recorded zero is its own row, not a blank entry.
var exportHeader = []string{
	"type", "occurred_at", "day", "value", "unit", "duration_seconds", "duration", "note", "recorded_by", "via_link",
}

func (a *app) handleExportTracker(w http.ResponseWriter, r *http.Request) {
	t, ok := a.ownedTracker(w, r)
	if !ok {
		return
	}
	loc := location(currentUser(r).TimeZone)
	var entries []Entry
	if err := a.db.Where("tracker_id = ?", t.ID).Order("occurred_at, id").Find(&entries).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	var zeros []RecordedZero
	if err := a.db.Where("tracker_id = ?", t.ID).Order("day").Find(&zeros).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	emails, err := a.emailsFor(entries)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	for _, z := range zeros {
		if z.RecordedByID != nil {
			if _, ok := emails[*z.RecordedByID]; !ok {
				var u User
				if err := a.db.Select("id, email").Take(&u, *z.RecordedByID).Error; err == nil {
					emails[u.ID] = u.Email
				}
			}
		}
	}
	rows, err := exportRows(t, entries, zeros, emails, loc)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(exportFilename(t)))
	cw := csv.NewWriter(w)
	if err := cw.Write(exportHeader); err != nil {
		return
	}
	if err := cw.WriteAll(rows); err != nil {
		return
	}
}

func exportFilename(t Tracker) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(t.Name) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	base := b.String()
	if base == "" {
		base = fmt.Sprintf("tracker-%d", t.ID)
	}
	return base + ".csv"
}

type exportItem struct {
	at  time.Time
	row []string
}

func exportRows(t Tracker, entries []Entry, zeros []RecordedZero, emails map[uint]string, loc *time.Location) ([][]string, error) {
	items := make([]exportItem, 0, len(entries)+len(zeros))
	for _, e := range entries {
		value, unit, seconds, words := "", "", "", ""
		if e.Number != nil {
			value = formatNumber(*e.Number)
			unit = t.Unit
		}
		if e.DurationSec != nil {
			seconds = strconv.Itoa(*e.DurationSec)
			words = formatDuration(*e.DurationSec)
		}
		items = append(items, exportItem{
			at: e.OccurredAt,
			row: []string{
				"entry",
				e.OccurredAt.UTC().Format(time.RFC3339),
				localDay(e.OccurredAt, loc),
				value, unit, seconds, words,
				e.Note,
				recordedBy(e.RecordedByID, emails),
				csvBool(e.ViaLink),
			},
		})
	}
	for _, z := range zeros {
		start, _, err := dayBounds(z.Day, loc)
		if err != nil {
			return nil, err
		}
		items = append(items, exportItem{
			at: start,
			row: []string{
				"recorded_zero",
				"",
				z.Day,
				"", "", "", "",
				"",
				recordedBy(z.RecordedByID, emails),
				csvBool(z.ViaLink),
			},
		})
	}
	// Oldest first. A zero and an entry at the same instant keep the entry first.
	for i := 1; i < len(items); i++ {
		j := i
		for j > 0 && exportBefore(items[j], items[j-1]) {
			items[j], items[j-1] = items[j-1], items[j]
			j--
		}
	}
	rows := make([][]string, len(items))
	for i, item := range items {
		rows[i] = item.row
	}
	return rows, nil
}

func exportBefore(a, b exportItem) bool {
	if a.at.Equal(b.at) {
		return a.row[0] == "entry" && b.row[0] != "entry"
	}
	return a.at.Before(b.at)
}

func recordedBy(id *uint, emails map[uint]string) string {
	if id == nil {
		return ""
	}
	return emails[*id]
}

func csvBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
