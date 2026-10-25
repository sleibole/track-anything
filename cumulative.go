package main

import (
	"math/big"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// AddLabel is the cumulative entry button. A custom log label wins.
// A unit made of letters, such as "points", reads as "Add points".
// Anything else, including an empty unit, is "Add amount".
func (t Tracker) AddLabel() string {
	if t.LogLabel != "" {
		return t.LogLabel
	}
	if practicalUnit(t.Unit) {
		return "Add " + t.Unit
	}
	return "Add amount"
}

func practicalUnit(unit string) bool {
	unit = strings.TrimSpace(unit)
	if unit == "" {
		return false
	}
	for _, r := range unit {
		if r != ' ' && !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// ratFromFloat uses the decimal the app already shows for a stored number.
// Sums and remainders of those decimals stay exact, so a total that lands on
// a goal has zero progress instead of a binary rounding leftover.
func ratFromFloat(v float64) *big.Rat {
	r := new(big.Rat)
	if _, ok := r.SetString(strconv.FormatFloat(v, 'f', -1, 64)); ok {
		return r
	}
	r.SetFloat64(v)
	return r
}

func sumRats(amounts []float64) *big.Rat {
	total := new(big.Rat)
	for _, v := range amounts {
		total.Add(total, ratFromFloat(v))
	}
	return total
}

// formatRat prints a rational as a decimal without trailing zeros.
// Amounts come from decimal strings, so the result terminates.
func formatRat(r *big.Rat) string {
	if r == nil || r.Sign() == 0 {
		return "0"
	}
	neg := r.Sign() < 0
	num := new(big.Int).Abs(r.Num())
	den := new(big.Int).Set(r.Denom())
	intPart := new(big.Int).Quo(num, den)
	rem := new(big.Int).Rem(num, den)
	if rem.Sign() == 0 {
		if neg {
			return "-" + intPart.String()
		}
		return intPart.String()
	}
	frac := make([]byte, 0, 8)
	for rem.Sign() != 0 && len(frac) < 340 {
		rem.Mul(rem, big.NewInt(10))
		digit := new(big.Int).Quo(rem, den)
		rem.Rem(rem, den)
		frac = append(frac, byte('0'+digit.Int64()))
	}
	if rem.Sign() == 0 {
		for len(frac) > 0 && frac[len(frac)-1] == '0' {
			frac = frac[:len(frac)-1]
		}
	}
	s := intPart.String() + "." + string(frac)
	if neg {
		return "-" + s
	}
	return s
}

func formatAmount(number, unit string) string {
	if unit == "" {
		return number
	}
	return number + " " + unit
}

// goalsCompleted is floor(total / goal) for a positive goal and a non-negative total.
func goalsCompleted(total, goal *big.Rat) *big.Int {
	if goal == nil || goal.Sign() <= 0 || total == nil || total.Sign() <= 0 {
		return big.NewInt(0)
	}
	q := new(big.Rat).Quo(total, goal)
	return new(big.Int).Quo(q.Num(), q.Denom())
}

func goalProgress(total, goal *big.Rat, completed *big.Int) *big.Rat {
	if goal == nil || completed == nil || completed.Sign() == 0 {
		if total == nil {
			return new(big.Rat)
		}
		return new(big.Rat).Set(total)
	}
	used := new(big.Rat).Mul(new(big.Rat).SetInt(completed), goal)
	return new(big.Rat).Sub(total, used)
}

type progressState struct {
	Headline  string
	HasGoal   bool
	Progress  string
	Goal      string
	Completed string
	Earned    string
}

func progressStateOf(amounts []float64, goal *float64, unit string) progressState {
	total := sumRats(amounts)
	totalText := formatAmount(formatRat(total), unit)
	if goal == nil || *goal <= 0 {
		return progressState{Headline: totalText}
	}
	g := ratFromFloat(*goal)
	done := goalsCompleted(total, g)
	prog := formatRat(goalProgress(total, g, done))
	goalText := formatRat(g)
	return progressState{
		Headline:  prog + " / " + formatAmount(goalText, unit),
		HasGoal:   true,
		Progress:  prog,
		Goal:      goalText,
		Completed: goalsLabel(done),
		Earned:    totalText + " total earned",
	}
}

func goalsLabel(n *big.Int) string {
	if n != nil && n.Cmp(big.NewInt(1)) == 0 {
		return "1 goal completed"
	}
	if n == nil {
		return "0 goals completed"
	}
	return n.String() + " goals completed"
}

// completedDelta is how many goals the new amounts finished.
// An empty string means the completed count did not rise.
func completedDelta(before, after []float64, goal float64) string {
	if goal <= 0 {
		return ""
	}
	g := ratFromFloat(goal)
	old := goalsCompleted(sumRats(before), g)
	next := goalsCompleted(sumRats(after), g)
	if next.Cmp(old) <= 0 {
		return ""
	}
	return new(big.Int).Sub(next, old).String()
}

// parseAmount accepts zero and positive numbers. Negatives are rejected.
func parseAmount(raw string) (float64, string) {
	v, msg := parseNumber(raw)
	if msg != "" {
		return 0, msg
	}
	if v < 0 {
		return 0, "Enter zero or a positive number."
	}
	return v, ""
}

// parseGoal accepts a blank field as no goal. Zero and negatives are rejected.
func parseGoal(raw string) (*float64, string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, ""
	}
	v, msg := parseNumber(s)
	if msg != "" {
		return nil, msg
	}
	if v <= 0 {
		return nil, "Enter a goal greater than zero, or leave it blank."
	}
	return &v, ""
}

func goalInput(g *float64) string {
	if g == nil {
		return ""
	}
	return formatNumber(*g)
}

func goalForKind(kind, raw string) *float64 {
	if kind != kindCumulative {
		return nil
	}
	g, _ := parseGoal(raw)
	return g
}

// runningLabels is the lifetime total as of each entry, oldest first.
// Same occurred_at keeps the smaller id first.
func runningLabels(entries []Entry, unit string) map[uint]string {
	sorted := append([]Entry(nil), entries...)
	slices.SortFunc(sorted, func(a, b Entry) int {
		if c := a.OccurredAt.Compare(b.OccurredAt); c != 0 {
			return c
		}
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})
	total := new(big.Rat)
	out := make(map[uint]string, len(sorted))
	for _, e := range sorted {
		if e.Number == nil {
			continue
		}
		total.Add(total, ratFromFloat(*e.Number))
		out[e.ID] = formatAmount(formatRat(total), unit) + " total"
	}
	return out
}

func stampRunning(views []entryView, labels map[uint]string) {
	if len(labels) == 0 {
		return
	}
	for i := range views {
		if s, ok := labels[views[i].ID]; ok {
			views[i].Running = s
		}
	}
}

// historySubtotalNote labels the sum of entries in the visible history
// when older entries exist. The lifetime total stays the headline.
func historySubtotalNote(entries []Entry, from time.Time, unit string) string {
	outside := false
	var window []float64
	for _, e := range entries {
		if e.Number == nil {
			continue
		}
		if e.OccurredAt.Before(from) {
			outside = true
			continue
		}
		window = append(window, *e.Number)
	}
	if !outside {
		return ""
	}
	return "In this history: " + formatAmount(formatRat(sumRats(window)), unit) + ". The total above includes every entry."
}

func goalNoticeText(n string) string {
	n = strings.TrimLeft(n, "0")
	if n == "" {
		return ""
	}
	if n == "1" {
		return "Goal completed!"
	}
	return n + " goals completed!"
}

// goalNoticeFrom reads the one-time result of an entry that finished goals.
// A page without that query does not celebrate.
func goalNoticeFrom(q url.Values, trackerID uint) string {
	if q.Get("notice") != "goal" {
		return ""
	}
	if raw := q.Get("tracker"); raw != "" && parseID(raw) != trackerID {
		return ""
	}
	n := q.Get("n")
	if n == "" || len(n) > 18 {
		return ""
	}
	for _, c := range n {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return goalNoticeText(n)
}

func withNotice(raw string, trackerID uint, n string) string {
	if n == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("notice", "goal")
	q.Set("n", n)
	q.Set("tracker", strconv.FormatUint(uint64(trackerID), 10))
	u.RawQuery = q.Encode()
	return u.String()
}
