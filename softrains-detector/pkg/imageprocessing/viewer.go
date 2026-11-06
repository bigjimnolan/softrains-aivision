package imageprocessing

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"softrains-detector/pkg/softrainsvision"
	"softrains-detector/pkg/startup"

	"gocv.io/x/gocv"
)

// viewer package runs the display loop and has a channel that listens for new frame requests.
// It takes those requests and returns the current frame. It also listens for returned frames
// do draw bounding boxes on them before displaying.
/*
	green := color.RGBA{G: 255, A: 255}
	black := color.RGBA{A: 255}
	lastDetections := []softrainsvision.Detection{}
	lastDetectErr := ""
*/

var poseKeyMap = map[string]string{
	"nose":           "n",
	"left_eye":       "le",
	"right_eye":      "re",
	"left_ear":       "lar",
	"right_ear":      "rar",
	"left_shoulder":  "ls",
	"right_shoulder": "rs",
	"left_elbow":     "le",
	"right_elbow":    "re",
	"left_wrist":     "lw",
	"right_wrist":    "rw",
	"left_hip":       "lh",
	"right_hip":      "rh",
	"left_knee":      "lk",
	"right_knee":     "rk",
	"left_ankle":     "la",
	"right_ankle":    "ra",
}

func (v *DetectionOnlyService) ShowFrames() error {
	frame := gocv.NewMat()
	defer frame.Close()
	frameRGB := gocv.NewMat()
	defer frameRGB.Close()
	orientationBuffer := gocv.NewMat()
	defer orientationBuffer.Close()
	window := gocv.NewWindow("Detection Viewer")
	defer window.Close()
	count := 0
	for detectionReturn := range v.viewerChannel {
		//add image to mat
		img := detectionReturn.Image
		if img == nil {
			v.logger.Warn("Received nil image from detector")
			continue
		}

		frame, err := gocv.ImageToMatRGB(img)
		if err != nil {
			v.logger.Errorf("Failed to convert image to Mat: %v", err)
			continue
		}
		gocv.CvtColor(frame, &frameRGB, gocv.ColorBGRToRGB)

		// Draw overlays on the frame
		v.drawOverlays(&frameRGB, detectionReturn.Detections, "Human", count, "")

		window.IMShow(frameRGB)
		if window.WaitKey(1) >= 0 {
			break
		}
		count++
	}
	return nil
}

func (v *DetectionOnlyService) drawOverlays(frame *gocv.Mat, detections []softrainsvision.Detection, targetType string, frameNumber int, lastErr string) {
	stroke := color.RGBA{R: 0, G: 255, B: 0, A: 255} // bright green for boxes and text
	textBg := color.RGBA{R: 0, G: 0, B: 0, A: 128}   // semi-transparent black for text backgrounds
	frameW := frame.Cols()
	frameH := frame.Rows()

	trackingViewport := softrainsvision.DetectionViewport(frame.Cols(), frame.Rows(), startup.CameraType(v.startupProfile.BotSetup.Connection.CameraType))
	if trackingViewport.Empty() {
		trackingViewport = image.Rect(0, 0, frameW, frameH)
	}
	centerX := trackingViewport.Min.X + trackingViewport.Dx()/2
	centerY := trackingViewport.Min.Y + trackingViewport.Dy()/2

	// Draw center crosshair and deadzone for quick visual alignment.
	gocv.Line(frame, image.Pt(centerX-20, centerY), image.Pt(centerX+20, centerY), stroke, 1)
	gocv.Line(frame, image.Pt(centerX, centerY-20), image.Pt(centerX, centerY+20), stroke, 1)
	deadZone := v.startupProfile.BotSetup.Tracking.DeadZone
	if deadZone > 0 {
		gocv.Rectangle(frame, imageRect(centerX-deadZone, centerY-deadZone, centerX+deadZone, centerY+deadZone), stroke, 1)
	}
	if trackingViewport.Dx() != frameW || trackingViewport.Dy() != frameH {
		gocv.Rectangle(frame, trackingViewport, stroke, 1)
	}

	poseColor := color.RGBA{R: 0, G: 200, B: 255, A: 255} // cyan for keypoints

	for _, detection := range detections {
		rect := detection.BoundingBox
		gocv.Rectangle(frame, rect, stroke, 2)
		label := fmt.Sprintf("human %.2f", detection.Confidence)
		labelPoint := rect.Min
		labelPoint.Y -= 8
		if labelPoint.Y < 20 {
			labelPoint.Y = rect.Min.Y + 20
		}
		textRect := imageRect(labelPoint.X-2, labelPoint.Y-16, labelPoint.X+120, labelPoint.Y+4)
		gocv.Rectangle(frame, textRect, textBg, -1)
		gocv.PutText(frame, label, labelPoint, gocv.FontHersheySimplex, 0.5, stroke, 1)

		// Pose keypoints are normalized to the detection crop, so map them back
		// into the detection rectangle in full-frame coordinates.
		boxW := rect.Dx()
		boxH := rect.Dy()
		for name, shortName := range poseKeyMap {
			kp, ok := detection.Pose[name]
			if !ok {
				continue
			}
			if kp.Confidence < 0.3 {
				continue
			}
			relX := clamp01(kp.X)
			relY := clamp01(kp.Y)
			px := rect.Min.X + int(relX*float32(boxW))
			py := rect.Min.Y + int(relY*float32(boxH))
			px = clampInt(px, 0, frameW-1)
			py = clampInt(py, 0, frameH-1)
			gocv.Circle(frame, image.Pt(px, py), 4, poseColor, -1)

			labelPt := image.Pt(px+5, py-5)
			labelBg := imageRect(labelPt.X-2, labelPt.Y-11, labelPt.X+28, labelPt.Y+3)
			gocv.Rectangle(frame, labelBg, textBg, -1)
			gocv.PutText(frame, shortName, labelPt, gocv.FontHersheySimplex, 0.35, poseColor, 1)
		}
	}
	lines := []string{
		fmt.Sprintf("Hdg: %f | Tgt: %s | Cnt: %v", v.panProvider.GetPan(), targetType, frameNumber),
	}

	if lastErr != "" {
		lines = append(lines, fmt.Sprintf("Detect error: %s", lastErr))
	}

	y := 24
	for _, line := range lines {
		textRect := imageRect(8, y-16, 480, y+4)
		gocv.Rectangle(frame, textRect, textBg, -1)
		gocv.PutText(frame, line, imagePoint(12, y), gocv.FontHersheySimplex, 0.6, stroke, 1)
		y += 24
	}
}

func clamp01(v float32) float32 {
	return float32(math.Max(0, math.Min(1, float64(v))))
}

func clampInt(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func imageRect(minX, minY, maxX, maxY int) image.Rectangle {
	return image.Rect(minX, minY, maxX, maxY)
}

func imagePoint(x, y int) image.Point {
	return image.Pt(x, y)
}
