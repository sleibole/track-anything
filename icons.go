package main

import (
	"embed"
	"fmt"
	"html/template"
	"slices"
)

// Tabler icons (MIT, see icons/LICENSE). Each SVG was copied in with aria-hidden="true"
// and class="icon" added, so icon-only buttons need their own aria-label.
//
//go:embed icons/*.svg
var iconFS embed.FS

// icon inlines an SVG so its stroke="currentColor" follows the surrounding text color.
func icon(name string) (template.HTML, error) {
	b, err := iconFS.ReadFile("icons/" + name + ".svg")
	if err != nil {
		return "", fmt.Errorf("icon %q: %w", name, err)
	}
	return template.HTML(b), nil
}

type choice struct {
	Value string
	Label string
}

// trackerIcons is the picker: a few Tabler names, then a few emoji. The empty value is
// the tally mark. Removing an entry is safe; stored values off the list render as the tally mark.
var trackerIcons = []choice{
	{"", "Tally marks"},
	{"paw", "Paw"},
	{"bone", "Bone"},
	{"pill", "Pill"},
	{"trees", "Trees"},
	{"walk", "Walk"},
	{"barbell", "Barbell"},
	{"droplet", "Droplet"},
	{"moon", "Moon"},
	{"baby-bottle", "Baby bottle"},
	{"baby-carriage", "Stroller"},
	{"toilet-paper", "Toilet paper"},
	{"bath", "Bath"},
	{"spray", "Spray"},
	{"wash-machine", "Washing machine"},
	{"ironing", "Iron"},
	{"wash", "Wash"},
	{"hand-sanitizer", "Sanitizer"},
	{"plunger", "Plunger"},
	{"bed", "Bed"},
	{"glass-full", "Drink"},
	{"coffee", "Coffee"},
	{"temperature", "Thermometer"},
	{"dental", "Tooth"},
	{"scale", "Scale"},
	{"book", "Book"},
	{"bike", "Bike"},
	{"🐶", "Dog"},
	{"🥣", "Bowl"},
	{"💊", "Medicine"},
	{"🌳", "Tree"},
	{"💪", "Strength"},
	{"😴", "Sleep"},
	{"🍼", "Bottle"},
	{"🤱", "Breastfeeding"},
	{"👶", "Baby"},
	{"🧷", "Diaper"},
	{"💩", "Poop"},
	{"🛁", "Bath"},
	{"🦷", "Teeth"},
	{"💧", "Water"},
	{"☕", "Coffee"},
	{"📖", "Reading"},
	{"🏃", "Run"},
}

// trackerAccents are soft tints defined in app.css as .accent-<value>.
var trackerAccents = []choice{
	{"", "None"},
	{"blue", "Blue"},
	{"green", "Green"},
	{"amber", "Amber"},
	{"rose", "Rose"},
	{"violet", "Violet"},
}

func listed(choices []choice, v string) bool {
	return slices.ContainsFunc(choices, func(c choice) bool { return c.Value == v })
}

func isEmoji(v string) bool {
	return v != "" && v[0] >= 0x80
}

// trackerIcon draws a tracker's icon: an allow-listed Tabler SVG, an emoji as text,
// or the tally mark for empty and no-longer-listed values.
func trackerIcon(v string) (template.HTML, error) {
	if !listed(trackerIcons, v) || v == "" {
		return icon("tallymarks")
	}
	if isEmoji(v) {
		return template.HTML(`<span class="emoji" aria-hidden="true">` + template.HTMLEscapeString(v) + `</span>`), nil
	}
	return icon(v)
}

var templateFuncs = template.FuncMap{
	"icon":        icon,
	"trackerIcon": trackerIcon,
}
