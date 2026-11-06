package softrainsvision

import (
	"image"
	"strings"
)

const (
	ColorModeDominant      = "dominant"
	ColorModeShirtDominant = "shirt-dominant"
)

// AnalyzeDetectionColor returns the dominant named color in a detection region.
// mode selects whether to use the full bounding box or a shirt-focused sub-region.
func AnalyzeDetectionColor(frame image.Image, bbox image.Rectangle, mode string, sampleStep int) (string, bool) {
	bounds := frame.Bounds()
	region := bbox.Intersect(bounds)
	if region.Empty() {
		return "", false
	}

	normalizedMode := strings.ToLower(strings.TrimSpace(mode))
	if normalizedMode == "shirt" {
		normalizedMode = ColorModeShirtDominant
	}
	if normalizedMode == "" {
		normalizedMode = ColorModeDominant
	}
	if normalizedMode == ColorModeShirtDominant {
		region = shirtRegion(region)
		if region.Empty() {
			return "", false
		}
	}

	if sampleStep <= 0 {
		sampleStep = 4
	}

	counts := map[string]int{}
	samples := 0
	for y := region.Min.Y; y < region.Max.Y; y += sampleStep {
		for x := region.Min.X; x < region.Max.X; x += sampleStep {
			r, g, b, _ := frame.At(x, y).RGBA()
			name := classifyColor(uint8(r>>8), uint8(g>>8), uint8(b>>8))
			counts[name]++
			samples++
		}
	}
	if samples == 0 {
		return "", false
	}

	bestName := ""
	bestCount := -1
	for name, count := range counts {
		if count > bestCount {
			bestName = name
			bestCount = count
		}
	}
	if bestName == "" {
		return "", false
	}
	return bestName, true
}

func shirtRegion(bbox image.Rectangle) image.Rectangle {
	w := bbox.Dx()
	h := bbox.Dy()
	if w <= 0 || h <= 0 {
		return image.Rectangle{}
	}

	// Focus on torso area: centered width, upper-mid height.
	left := bbox.Min.X + int(0.2*float64(w))
	right := bbox.Min.X + int(0.8*float64(w))
	top := bbox.Min.Y + int(0.25*float64(h))
	bottom := bbox.Min.Y + int(0.7*float64(h))

	if right <= left || bottom <= top {
		return image.Rectangle{}
	}
	return image.Rect(left, top, right, bottom)
}

func classifyColor(r, g, b uint8) string {
	h, s, v := rgbToHSV(r, g, b)

	if v < 0.18 {
		return "black"
	}
	if s < 0.12 {
		if v > 0.82 {
			return "white"
		}
		return "gray"
	}

	switch {
	case h < 15 || h >= 345:
		return "red"
	case h < 40:
		return "orange"
	case h < 65:
		return "yellow"
	case h < 160:
		return "green"
	case h < 200:
		return "cyan"
	case h < 260:
		return "blue"
	case h < 300:
		return "purple"
	default:
		return "pink"
	}
}

func rgbToHSV(r, g, b uint8) (float64, float64, float64) {
	rf := float64(r) / 255.0
	gf := float64(g) / 255.0
	bf := float64(b) / 255.0

	maxv := maxf(rf, gf, bf)
	minv := minf(rf, gf, bf)
	delta := maxv - minv

	var h float64
	if delta == 0 {
		h = 0
	} else if maxv == rf {
		h = 60 * mod6((gf-bf)/delta)
	} else if maxv == gf {
		h = 60 * (((bf - rf) / delta) + 2)
	} else {
		h = 60 * (((rf - gf) / delta) + 4)
	}
	if h < 0 {
		h += 360
	}

	s := 0.0
	if maxv > 0 {
		s = delta / maxv
	}
	v := maxv
	return h, s, v
}

func mod6(v float64) float64 {
	for v < 0 {
		v += 6
	}
	for v >= 6 {
		v -= 6
	}
	return v
}

func maxf(a, b, c float64) float64 {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	return m
}

func minf(a, b, c float64) float64 {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
