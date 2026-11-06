package softrainsvision

import (
	"image"
	"time"
)

// DeviceType specifies the compute device for inference
type DeviceType int

const (
	DeviceCPU DeviceType = iota
	DeviceCoralTPU
)

// Keypoint is a single pose landmark with normalised image coordinates.
type Keypoint struct {
	X          float32
	Y          float32
	Confidence float32
}

// Pose holds the 17 COCO-topology keypoints produced by MoveNet.
type Pose map[string]Keypoint

// Detection represents a detected object in an image
type Detection struct {
	BoundingBox   image.Rectangle
	Confidence    float32
	Center        image.Point
	Pose          Pose // nil when pose was not requested or not available
	DominantColor string
	ColorMode     string
}

// DetectionResult represents raw detection output from the model
type DetectionResult struct {
	X          float32
	Y          float32
	Width      float32
	Height     float32
	Confidence float32
	ClassID    int
}

// TrackingTarget represents a detected and tracked object over time
type TrackingTarget struct {
	Detection  Detection
	LastSeen   time.Time
	TrackingID int
	Velocity   image.Point
	LastPanPos float64 // Pan position where this target was last detected; 0 means unknown
}

type DetectionReturn struct {
	DetectionPoint image.Point
	PanAtDetection float64
	Detections     []Detection
	Image          image.Image // The raw image used
}
