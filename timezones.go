package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// friendlyTimeZones is what Settings offers, modeled on Rails' ActiveSupport::TimeZone
// names. Each IANA name appears once, so the stored value maps back to one option.
var friendlyTimeZones = []struct{ Name, Zone string }{
	{"International Date Line West", "Etc/GMT+12"},
	{"Midway Island", "Pacific/Midway"},
	{"American Samoa", "Pacific/Pago_Pago"},
	{"Hawaii", "Pacific/Honolulu"},
	{"Alaska", "America/Juneau"},
	{"Pacific Time (US & Canada)", "America/Los_Angeles"},
	{"Tijuana", "America/Tijuana"},
	{"Mountain Time (US & Canada)", "America/Denver"},
	{"Arizona", "America/Phoenix"},
	{"Chihuahua", "America/Chihuahua"},
	{"Mazatlan", "America/Mazatlan"},
	{"Central Time (US & Canada)", "America/Chicago"},
	{"Saskatchewan", "America/Regina"},
	{"Guadalajara, Mexico City", "America/Mexico_City"},
	{"Monterrey", "America/Monterrey"},
	{"Central America", "America/Guatemala"},
	{"Eastern Time (US & Canada)", "America/New_York"},
	{"Indiana (East)", "America/Indiana/Indianapolis"},
	{"Bogota", "America/Bogota"},
	{"Lima, Quito", "America/Lima"},
	{"Atlantic Time (Canada)", "America/Halifax"},
	{"Caracas", "America/Caracas"},
	{"La Paz", "America/La_Paz"},
	{"Santiago", "America/Santiago"},
	{"Newfoundland", "America/St_Johns"},
	{"Brasilia", "America/Sao_Paulo"},
	{"Buenos Aires", "America/Argentina/Buenos_Aires"},
	{"Montevideo", "America/Montevideo"},
	{"Georgetown", "America/Guyana"},
	{"Puerto Rico", "America/Puerto_Rico"},
	{"Greenland", "America/Nuuk"},
	{"Mid-Atlantic", "Atlantic/South_Georgia"},
	{"Azores", "Atlantic/Azores"},
	{"Cape Verde Is.", "Atlantic/Cape_Verde"},
	{"Dublin", "Europe/Dublin"},
	{"Edinburgh, London", "Europe/London"},
	{"Lisbon", "Europe/Lisbon"},
	{"Casablanca", "Africa/Casablanca"},
	{"Monrovia", "Africa/Monrovia"},
	{"UTC", "UTC"},
	{"Belgrade", "Europe/Belgrade"},
	{"Bratislava", "Europe/Bratislava"},
	{"Budapest", "Europe/Budapest"},
	{"Ljubljana", "Europe/Ljubljana"},
	{"Prague", "Europe/Prague"},
	{"Sarajevo", "Europe/Sarajevo"},
	{"Skopje", "Europe/Skopje"},
	{"Warsaw", "Europe/Warsaw"},
	{"Zagreb", "Europe/Zagreb"},
	{"Brussels", "Europe/Brussels"},
	{"Copenhagen", "Europe/Copenhagen"},
	{"Madrid", "Europe/Madrid"},
	{"Paris", "Europe/Paris"},
	{"Amsterdam", "Europe/Amsterdam"},
	{"Berlin", "Europe/Berlin"},
	{"Bern, Zurich", "Europe/Zurich"},
	{"Rome", "Europe/Rome"},
	{"Stockholm", "Europe/Stockholm"},
	{"Vienna", "Europe/Vienna"},
	{"West Central Africa", "Africa/Algiers"},
	{"Bucharest", "Europe/Bucharest"},
	{"Cairo", "Africa/Cairo"},
	{"Helsinki", "Europe/Helsinki"},
	{"Kyiv", "Europe/Kyiv"},
	{"Riga", "Europe/Riga"},
	{"Sofia", "Europe/Sofia"},
	{"Tallinn", "Europe/Tallinn"},
	{"Vilnius", "Europe/Vilnius"},
	{"Athens", "Europe/Athens"},
	{"Istanbul", "Europe/Istanbul"},
	{"Minsk", "Europe/Minsk"},
	{"Jerusalem", "Asia/Jerusalem"},
	{"Harare", "Africa/Harare"},
	{"Pretoria", "Africa/Johannesburg"},
	{"Kaliningrad", "Europe/Kaliningrad"},
	{"Moscow, St. Petersburg", "Europe/Moscow"},
	{"Volgograd", "Europe/Volgograd"},
	{"Samara", "Europe/Samara"},
	{"Kuwait", "Asia/Kuwait"},
	{"Riyadh", "Asia/Riyadh"},
	{"Nairobi", "Africa/Nairobi"},
	{"Baghdad", "Asia/Baghdad"},
	{"Tehran", "Asia/Tehran"},
	{"Abu Dhabi, Muscat", "Asia/Muscat"},
	{"Baku", "Asia/Baku"},
	{"Tbilisi", "Asia/Tbilisi"},
	{"Yerevan", "Asia/Yerevan"},
	{"Kabul", "Asia/Kabul"},
	{"Ekaterinburg", "Asia/Yekaterinburg"},
	{"Islamabad, Karachi", "Asia/Karachi"},
	{"Tashkent", "Asia/Tashkent"},
	{"Chennai, Kolkata, Mumbai, New Delhi", "Asia/Kolkata"},
	{"Kathmandu", "Asia/Kathmandu"},
	{"Dhaka", "Asia/Dhaka"},
	{"Sri Jayawardenepura", "Asia/Colombo"},
	{"Almaty", "Asia/Almaty"},
	{"Novosibirsk", "Asia/Novosibirsk"},
	{"Yangon", "Asia/Yangon"},
	{"Bangkok, Hanoi", "Asia/Bangkok"},
	{"Jakarta", "Asia/Jakarta"},
	{"Krasnoyarsk", "Asia/Krasnoyarsk"},
	{"Beijing", "Asia/Shanghai"},
	{"Hong Kong", "Asia/Hong_Kong"},
	{"Urumqi", "Asia/Urumqi"},
	{"Kuala Lumpur", "Asia/Kuala_Lumpur"},
	{"Singapore", "Asia/Singapore"},
	{"Taipei", "Asia/Taipei"},
	{"Perth", "Australia/Perth"},
	{"Irkutsk", "Asia/Irkutsk"},
	{"Ulaanbaatar", "Asia/Ulaanbaatar"},
	{"Seoul", "Asia/Seoul"},
	{"Osaka, Sapporo, Tokyo", "Asia/Tokyo"},
	{"Yakutsk", "Asia/Yakutsk"},
	{"Darwin", "Australia/Darwin"},
	{"Adelaide", "Australia/Adelaide"},
	{"Canberra, Sydney", "Australia/Sydney"},
	{"Melbourne", "Australia/Melbourne"},
	{"Brisbane", "Australia/Brisbane"},
	{"Hobart", "Australia/Hobart"},
	{"Vladivostok", "Asia/Vladivostok"},
	{"Guam", "Pacific/Guam"},
	{"Port Moresby", "Pacific/Port_Moresby"},
	{"Magadan", "Asia/Magadan"},
	{"Srednekolymsk", "Asia/Srednekolymsk"},
	{"Solomon Is.", "Pacific/Guadalcanal"},
	{"New Caledonia", "Pacific/Noumea"},
	{"Fiji", "Pacific/Fiji"},
	{"Kamchatka", "Asia/Kamchatka"},
	{"Marshall Is.", "Pacific/Majuro"},
	{"Auckland, Wellington", "Pacific/Auckland"},
	{"Nuku'alofa", "Pacific/Tongatapu"},
	{"Tokelau Is.", "Pacific/Fakaofo"},
	{"Chatham Is.", "Pacific/Chatham"},
	{"Samoa", "Pacific/Apia"},
}

var friendlyTimeZoneNames = func() map[string]string {
	m := make(map[string]string, len(friendlyTimeZones))
	for _, z := range friendlyTimeZones {
		m[z.Zone] = z.Name
	}
	return m
}()

// listedTimeZone reports whether Settings offers this IANA name.
func listedTimeZone(zone string) bool {
	_, ok := friendlyTimeZoneNames[zone]
	return ok
}

// timeZoneLabel is the friendly name for a stored zone. Zones detected at signup that
// aren't listed fall back to their city, e.g. "America/Boise" is "Boise".
func timeZoneLabel(zone string) string {
	if name, ok := friendlyTimeZoneNames[zone]; ok {
		return name
	}
	city := zone[strings.LastIndex(zone, "/")+1:]
	return strings.ReplaceAll(city, "_", " ")
}

type timeZoneOption struct {
	Zone     string
	Label    string // "(GMT-07:00) Pacific Time (US & Canada)"
	Selected bool
	offset   int
}

// timeZoneOptions lists the friendly zones sorted by their UTC offset at now, plus the
// current zone if it isn't listed, so saving without a change keeps it.
func timeZoneOptions(current string, now time.Time) []timeZoneOption {
	opts := make([]timeZoneOption, 0, len(friendlyTimeZones)+1)
	add := func(zone string) {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			return
		}
		_, offset := now.In(loc).Zone()
		opts = append(opts, timeZoneOption{
			Zone:     zone,
			Label:    fmt.Sprintf("(%s) %s", formatGMTOffset(offset), timeZoneLabel(zone)),
			Selected: zone == current,
			offset:   offset,
		})
	}
	for _, z := range friendlyTimeZones {
		add(z.Zone)
	}
	if !listedTimeZone(current) {
		add(current)
	}
	sort.SliceStable(opts, func(i, j int) bool {
		if opts[i].offset != opts[j].offset {
			return opts[i].offset < opts[j].offset
		}
		return opts[i].Label < opts[j].Label
	})
	return opts
}

func formatGMTOffset(seconds int) string {
	sign := '+'
	if seconds < 0 {
		sign = '-'
		seconds = -seconds
	}
	return fmt.Sprintf("GMT%c%02d:%02d", sign, seconds/3600, seconds%3600/60)
}
