package softrainsvision

import "image"

// HumanDetectionCriteria contains size/ratio filters shared by detector backends.
type HumanDetectionCriteria struct {
	ConfidenceThreshold float32
	MinBoxWidth         int
	MinBoxHeight        int
	MinBoxArea          int
	MaxAreaRatio        float32
	MaxWidthRatio       float32
	MaxHeightRatio      float32
}

// BuildHumanDetection converts normalized box coordinates into frame coordinates
// and applies all configured filters. Returns false when filtered out.
func BuildHumanDetection(
	confidence float32,
	normX float32,
	normY float32,
	normWidth float32,
	normHeight float32,
	frameWidth int,
	frameHeight int,
	criteria HumanDetectionCriteria,
) (Detection, bool) {
	if confidence < criteria.ConfidenceThreshold {
		return Detection{}, false
	}

	x := int(normX * float32(frameWidth))
	y := int(normY * float32(frameHeight))
	w := int(normWidth * float32(frameWidth))
	h := int(normHeight * float32(frameHeight))

	if w < criteria.MinBoxWidth || h < criteria.MinBoxHeight {
		return Detection{}, false
	}

	boxArea := w * h
	if boxArea < criteria.MinBoxArea {
		return Detection{}, false
	}

	frameArea := frameWidth * frameHeight
	if float32(boxArea) > criteria.MaxAreaRatio*float32(frameArea) {
		return Detection{}, false
	}
	if float32(w) > criteria.MaxWidthRatio*float32(frameWidth) {
		return Detection{}, false
	}
	if float32(h) > criteria.MaxHeightRatio*float32(frameHeight) {
		return Detection{}, false
	}

	if x < 0 || y < 0 || x+w > frameWidth || y+h > frameHeight {
		x = max(0, x)
		y = max(0, y)
		w = min(w, frameWidth-x)
		h = min(h, frameHeight-y)
	}

	detection := Detection{
		BoundingBox: image.Rect(x, y, x+w, y+h),
		Confidence:  confidence,
		Center: image.Point{
			X: x + w/2,
			Y: y + h/2,
		},
	}
	return detection, true
}
