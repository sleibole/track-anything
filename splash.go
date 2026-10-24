package main

import (
	"bytes"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// splashScreen is one iPhone or iPad size, in portrait points and pixels.
// Sizes are the distinct screens from pwa-asset-generator 8.1.7. iOS shows a
// startup image only when its pixel size matches the device and orientation.
type splashScreen struct {
	CSSW, CSSH int
	DPR        int
	PXW, PXH   int
}

// splashScreens covers current iPhone and iPad full-screen sizes. Split View
// and Stage Manager windows match none of these, so those launches stay blank.
var splashScreens = []splashScreen{
	{440, 956, 3, 1320, 2868},   // iPhone 17 Pro Max, 16 Pro Max
	{420, 912, 3, 1260, 2736},   // iPhone Air
	{402, 874, 3, 1206, 2622},   // iPhone 17 Pro, 17, 16 Pro
	{430, 932, 3, 1290, 2796},   // iPhone 16 Plus, 15 Pro Max, 15 Plus, 14 Pro Max
	{393, 852, 3, 1179, 2556},   // iPhone 16, 15 Pro, 15, 14 Pro
	{428, 926, 3, 1284, 2778},   // iPhone 14 Plus, 13 Pro Max, 12 Pro Max
	{390, 844, 3, 1170, 2532},   // iPhone 16e, 14, 13 Pro, 13, 12 Pro, 12
	{375, 812, 3, 1125, 2436},   // iPhone 11 Pro, XS, X
	{360, 780, 3, 1080, 2340},   // iPhone 13 mini, 12 mini
	{414, 896, 3, 1242, 2688},   // iPhone 11 Pro Max, XS Max
	{414, 896, 2, 828, 1792},    // iPhone 11, XR
	{414, 736, 3, 1242, 2208},   // iPhone 8 Plus, 7 Plus, 6s Plus, 6 Plus
	{375, 667, 2, 750, 1334},    // iPhone SE (2nd and 3rd), 8, 7, 6s, 6
	{320, 568, 2, 640, 1136},    // iPhone SE (1st)
	{1032, 1376, 2, 2064, 2752}, // iPad Pro 13-inch
	{1024, 1366, 2, 2048, 2732}, // iPad Pro 12.9-inch, iPad Air 13-inch
	{834, 1210, 2, 1668, 2420},  // iPad Pro 11-inch (5th and 6th)
	{834, 1194, 2, 1668, 2388},  // iPad Pro 11-inch (1st–4th)
	{820, 1180, 2, 1640, 2360},  // iPad Air 11-inch and 10.9-inch, iPad 11-inch
	{834, 1112, 2, 1668, 2224},  // iPad Pro 10.5-inch, iPad Air 10.5-inch
	{810, 1080, 2, 1620, 2160},  // iPad 10.2-inch
	{744, 1133, 2, 1488, 2266},  // iPad mini 8.3-inch
	{768, 1024, 2, 1536, 2048},  // iPad 9.7-inch, iPad mini 7.9-inch
}

// Page backgrounds: white, and Pico's dark background painted as 8-bit color.
var (
	splashLight = color.RGBA{0xff, 0xff, 0xff, 0xff}
	splashDark  = color.RGBA{0x13, 0x17, 0x1f, 0xff}
)

var (
	splashIconOnce sync.Once
	splashIcon     image.Image
	splashIconErr  error

	splashMu    sync.Mutex
	splashCache sync.Map // "scheme/WxH" -> []byte PNG
)

func loadSplashIcon() (image.Image, error) {
	splashIconOnce.Do(func() {
		b, err := embedded.ReadFile("static/icons/icon-512.png")
		if err != nil {
			splashIconErr = err
			return
		}
		splashIcon, splashIconErr = png.Decode(bytes.NewReader(b))
	})
	return splashIcon, splashIconErr
}

func splashBackground(scheme string) (color.RGBA, bool) {
	switch scheme {
	case "light":
		return splashLight, true
	case "dark":
		return splashDark, true
	default:
		return color.RGBA{}, false
	}
}

func knownSplashSize(w, h int) bool {
	for _, s := range splashScreens {
		if (s.PXW == w && s.PXH == h) || (s.PXH == w && s.PXW == h) {
			return true
		}
	}
	return false
}

func parseSplashSize(size string) (int, int, bool) {
	name, ok := strings.CutSuffix(size, ".png")
	if !ok {
		return 0, 0, false
	}
	ws, hs, ok := strings.Cut(name, "x")
	if !ok || strings.Contains(hs, "x") {
		return 0, 0, false
	}
	w, errW := strconv.Atoi(ws)
	h, errH := strconv.Atoi(hs)
	if errW != nil || errH != nil || w < 1 || h < 1 {
		return 0, 0, false
	}
	return w, h, true
}

// splashPNG draws the 512 icon centered on the scheme background. The result
// is cached; iOS fetches a startup image when the home-screen icon is added.
func splashPNG(scheme string, w, h int) ([]byte, error) {
	bg, ok := splashBackground(scheme)
	if !ok || !knownSplashSize(w, h) {
		return nil, fmt.Errorf("unknown splash %s %dx%d", scheme, w, h)
	}
	key := scheme + "/" + strconv.Itoa(w) + "x" + strconv.Itoa(h)
	if v, ok := splashCache.Load(key); ok {
		return v.([]byte), nil
	}
	splashMu.Lock()
	defer splashMu.Unlock()
	if v, ok := splashCache.Load(key); ok {
		return v.([]byte), nil
	}
	icon, err := loadSplashIcon()
	if err != nil {
		return nil, err
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	ib := icon.Bounds()
	origin := image.Pt((w-ib.Dx())/2, (h-ib.Dy())/2)
	draw.Draw(dst, image.Rectangle{Min: origin, Max: origin.Add(ib.Size())}, icon, ib.Min, draw.Over)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	splashCache.Store(key, out)
	return out, nil
}

func handleSplash(w http.ResponseWriter, r *http.Request) {
	width, height, ok := parseSplashSize(r.PathValue("size"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	pngBytes, err := splashPNG(r.PathValue("scheme"), width, height)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(pngBytes)
}

// startupImageLinks is one apple-touch-startup-image per scheme and
// orientation. device-width and device-height stay in portrait for both.
func startupImageLinks() template.HTML {
	var b strings.Builder
	for _, s := range splashScreens {
		orients := []struct {
			name   string
			width  int
			height int
		}{
			{"portrait", s.PXW, s.PXH},
			{"landscape", s.PXH, s.PXW},
		}
		for _, orient := range orients {
			for _, scheme := range []string{"light", "dark"} {
				fmt.Fprintf(&b, `<link rel="apple-touch-startup-image" href="/static/splash/%s/%dx%d.png" media="(prefers-color-scheme: %s) and (device-width: %dpx) and (device-height: %dpx) and (-webkit-device-pixel-ratio: %d) and (orientation: %s)">`+"\n",
					scheme, orient.width, orient.height, scheme, s.CSSW, s.CSSH, s.DPR, orient.name)
			}
		}
	}
	return template.HTML(b.String())
}
