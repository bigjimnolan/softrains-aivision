Old Viewer code:
```
//go:build viewer && cgo

package source

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"softrains-detector/pkg/softrainsvision"
	"softrains-detector/pkg/startup"

	"github.com/sirupsen/logrus"
	"gocv.io/x/gocv"
)

var poseKeypointOrder = []string{
	"nose",
	"left_eye", "right_eye",
	"left_ear", "right_ear",
	"left_shoulder", "right_shoulder",
	"left_elbow", "right_elbow",
	"left_wrist", "right_wrist",
	"left_hip", "right_hip",
	"left_knee", "right_knee",
	"left_ankle", "right_ankle",
}

var poseKeypointShortName = map[string]string{
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

func Run(args []string) error {
	// --- Profile/config support ---
	configPathArg, configProvided := extractStringFlag(args, "config")
	profileArg, profileProvided := extractStringFlag(args, "profile")
	configPath := "config.json"
	if strings.TrimSpace(configPathArg) != "" {
		configPath = strings.TrimSpace(configPathArg)
	}
	var profileDefaults startup.ViewerStartupConfig
	var useProfile bool
	var err error
	if profileArg != "" || configPathArg != "" {
		// Only try to load if a profile/config is specified
		pd, _, perr := startup.LoadViewerStartupProfileFromFile(configPath, profileArg, configProvided, profileProvided)
		if perr == nil {
			profileDefaults = pd
			useProfile = true
		}
	}

	flags := flag.NewFlagSet("viewer", flag.ContinueOnError)
	// Accept --profile and --config as dummy flags so the parser doesn't error, even though we handle them manually
	_ = flags.String("profile", "", "(dummy, handled manually)")
	_ = flags.String("config", "", "(dummy, handled manually)")
	sourceType := flags.String("source", "rtsp", "Capture source type: rtsp or usb")
	rtspURL := flags.String("rtsp", "", "RTSP URL to display (required when --source=rtsp)")
	usbIndex := flags.Int("usb-index", 0, "USB camera index (used when --source=usb)")
	usbWidth := flags.Int("usb-width", 1280, "USB capture width")
	usbHeight := flags.Int("usb-height", 720, "USB capture height")
	usbFPS := flags.Int("usb-fps", 30, "USB capture FPS")
	cameraType := flags.String("camera-type", "standard", "Camera type: standard or stereo-left")
	rpcEndpoint := flags.String("rpc-endpoint", "127.0.0.1:8443", "RPC detector address")
	serverName := flags.String("server-name", "softrains.local", "TLS server name for certificate verification")
	caCertPath := flags.String("ca-cert", "", "Optional CA certificate PEM for the RPC endpoint")
	includePose := flags.Bool("include-pose", false, "Request pose inference from connector (requires server --enable-pose)")
	windowTitle := flags.String("window-title", "Softrains Viewer", "Window title")
	processEvery := flags.Int("process-every", 3, "Run detection every N frames")
	confidence := flags.Float64("confidence", 0.5, "Confidence threshold")
	nmsThreshold := flags.Float64("nms", 0.4, "NMS threshold")
	minBoxWidth := flags.Int("min-box-width", 20, "Minimum detection width in pixels")
	minBoxHeight := flags.Int("min-box-height", 40, "Minimum detection height in pixels")
	minBoxArea := flags.Int("min-box-area", 400, "Minimum detection area in pixels")
	maxAreaRatio := flags.Float64("max-area-ratio", 0.67, "Maximum frame area ratio for a detection")
	maxWidthRatio := flags.Float64("max-width-ratio", 0.9, "Maximum frame width ratio for a detection")
	maxHeightRatio := flags.Float64("max-height-ratio", 0.95, "Maximum frame height ratio for a detection")
	trackingDebug := flags.Bool("tracking-debug", true, "Render tracking target/adjustment debug overlay")
	trackingMode := flags.String("tracking-mode", "person", "Tracking target mode: person or pose")
	trackingDeadZone := flags.Int("tracking-deadzone", 20, "Deadzone in pixels around frame center")
	trackingPanSensitivity := flags.Float64("tracking-pan-sensitivity", 0.05, "Pan sensitivity used for adjustment preview")
	trackingPitchSensitivity := flags.Float64("tracking-pitch-sensitivity", 0.03, "Pitch sensitivity used for adjustment preview")
	trackingPanMin := flags.Float64("tracking-pan-min", -90, "Minimum pan angle")
	trackingPanMax := flags.Float64("tracking-pan-max", 90, "Maximum pan angle")
	trackingPitchMin := flags.Float64("tracking-pitch-min", -45, "Minimum tilt angle")
	trackingPitchMax := flags.Float64("tracking-pitch-max", 45, "Maximum tilt angle")
	trackingStartPan := flags.Float64("tracking-start-pan", 0, "Starting pan angle")
	trackingStartPitch := flags.Float64("tracking-start-pitch", 0, "Starting pitch angle")
	trackingInvertPan := flags.Bool("tracking-invert-pan", false, "Invert pan direction (use if camera steers away from subject)")
	trackingInvertTilt := flags.Bool("tracking-invert-tilt", false, "Invert tilt direction (use if camera steers away vertically)")
	poseLeftHandMinConfidence := flags.Float64("pose-left-hand-min-confidence", 0.35, "Minimum left wrist confidence in pose mode")
	controlURL := flags.String("control-url", "", "Gimbal base URL (e.g. http://192.168.68.104:8080); enables live axis commands")
	if err := flags.Parse(args); err != nil {
		return err
	}

	// Use profileDefaults if loaded, otherwise use flags
	getStr := func(p *string, fallback string) string {
		if useProfile && p != nil {
			return *p
		}
		return fallback
	}
	getInt := func(p *int, fallback int) int {
		if useProfile && p != nil {
			return *p
		}
		return fallback
	}
	getFlt := func(p *float64, fallback float64) float64 {
		if useProfile && p != nil {
			return *p
		}
		return fallback
	}
	getBool := func(p *bool, fallback bool) bool {
		if useProfile && p != nil {
			return *p
		}
		return fallback
	}

	resolvedSourceType := getStr(profileDefaults.Source, *sourceType)
	resolvedRTSP := getStr(profileDefaults.RTSP, *rtspURL)
	resolvedUSBIndex := getInt(profileDefaults.USBIndex, *usbIndex)
	resolvedUSBWidth := getInt(profileDefaults.USBWidth, *usbWidth)
	resolvedUSBHeight := getInt(profileDefaults.USBHeight, *usbHeight)
	resolvedUSBFPS := getInt(profileDefaults.USBFPS, *usbFPS)
	resolvedCameraType := getStr(profileDefaults.CameraType, *cameraType)
	resolvedRPCEndpoint := getStr(profileDefaults.RPCEndpoint, *rpcEndpoint)
	resolvedServerName := getStr(profileDefaults.ServerName, *serverName)
	resolvedCACert := getStr(profileDefaults.CACert, *caCertPath)
	resolvedIncludePose := getBool(profileDefaults.IncludePose, *includePose)
	resolvedWindowTitle := getStr(profileDefaults.WindowTitle, *windowTitle)
	resolvedProcessEvery := getInt(profileDefaults.ProcessEvery, *processEvery)
	resolvedConfidence := float32(getFlt(profileDefaults.Confidence, *confidence))
	resolvedNMSThreshold := float32(getFlt(profileDefaults.NMS, *nmsThreshold))
	resolvedMinBoxWidth := getInt(profileDefaults.MinBoxWidth, *minBoxWidth)
	resolvedMinBoxHeight := getInt(profileDefaults.MinBoxHeight, *minBoxHeight)
	resolvedMinBoxArea := getInt(profileDefaults.MinBoxArea, *minBoxArea)
	resolvedMaxAreaRatio := float32(getFlt(profileDefaults.MaxAreaRatio, *maxAreaRatio))
	resolvedMaxWidthRatio := float32(getFlt(profileDefaults.MaxWidthRatio, *maxWidthRatio))
	resolvedMaxHeightRatio := float32(getFlt(profileDefaults.MaxHeightRatio, *maxHeightRatio))
	resolvedTrackingDebug := getBool(profileDefaults.TrackingDebug, *trackingDebug)
	resolvedTrackingMode := getStr(profileDefaults.TrackingMode, *trackingMode)
	resolvedTrackingDeadZone := getInt(profileDefaults.TrackingDeadzone, *trackingDeadZone)
	resolvedTrackingPanSensitivity := getFlt(profileDefaults.TrackingPanSensitivity, *trackingPanSensitivity)
	resolvedTrackingPitchSensitivity := getFlt(profileDefaults.TrackingPitchSensitivity, *trackingPitchSensitivity)
	resolvedTrackingPanMin := getFlt(profileDefaults.TrackingPanMin, *trackingPanMin)
	resolvedTrackingPanMax := getFlt(profileDefaults.TrackingPanMax, *trackingPanMax)
	resolvedTrackingPitchMin := getFlt(profileDefaults.TrackingPitchMin, *trackingPitchMin)
	resolvedTrackingPitchMax := getFlt(profileDefaults.TrackingPitchMax, *trackingPitchMax)
	//	resolvedTrackingStartPan := getFlt(profileDefaults.TrackingStartPan, *trackingStartPan)
	//	resolvedTrackingStartPitch := getFlt(profileDefaults.TrackingStartPitch, *trackingStartPitch)
	resolvedTrackingInvertPan := getBool(profileDefaults.TrackingInvertPan, *trackingInvertPan)
	resolvedTrackingInvertTilt := getBool(profileDefaults.TrackingInvertTilt, *trackingInvertTilt)
	resolvedPoseLeftHandMinConfidence := getFlt(profileDefaults.PoseLeftHandMinConfidence, *poseLeftHandMinConfidence)
	resolvedControlURL := getStr(profileDefaults.ControlURL, *controlURL)

	// Validate required fields
	if resolvedSourceType != "rtsp" && resolvedSourceType != "usb" {
		return fmt.Errorf("--source must be either rtsp or usb")
	}
	if resolvedSourceType == "rtsp" && resolvedRTSP == "" {
		return fmt.Errorf("--rtsp is required when --source=rtsp")
	}
	if resolvedProcessEvery <= 0 {
		return fmt.Errorf("--process-every must be >= 1")
	}
	mode := strings.ToLower(resolvedTrackingMode)
	if mode != "person" && mode != "pose" {
		return fmt.Errorf("--tracking-mode must be either person or pose")
	}
	resolvedCameraType = normalizedCameraType(resolvedCameraType)

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	detector, err := softrainsvision.NewHumanDetectorRPC(
		resolvedRPCEndpoint,
		resolvedServerName,
		resolvedCACert,
		resolvedConfidence,
		resolvedNMSThreshold,
		resolvedMinBoxWidth,
		resolvedMinBoxHeight,
		resolvedMinBoxArea,
		resolvedMaxAreaRatio,
		resolvedMaxWidthRatio,
		resolvedMaxHeightRatio,
		logger,
	)
	if err != nil {
		return err
	}
	detector.SetIncludePose(resolvedIncludePose)
	defer detector.Close()

	capture, err := openCaptureSource(captureOptions{
		sourceType: resolvedSourceType,
		rtspURL:    resolvedRTSP,
		usbIndex:   resolvedUSBIndex,
		usbWidth:   resolvedUSBWidth,
		usbHeight:  resolvedUSBHeight,
		usbFPS:     resolvedUSBFPS,
	})
	if err != nil {
		return err
	}
	defer capture.Close()

	window := gocv.NewWindow(resolvedWindowTitle)
	defer window.Close()

	frame := gocv.NewMat()
	defer frame.Close()
	frameRGB := gocv.NewMat()
	defer frameRGB.Close()

	green := color.RGBA{G: 255, A: 255}
	red := color.RGBA{R: 255, A: 255}
	black := color.RGBA{A: 255}
	lastDetections := []softrainsvision.Detection{}
	lastDetectErr := ""
	trackingState := trackingDebugState{
		pan:   clampToRange(*trackingStartPan, *trackingPanMin, *trackingPanMax),
		pitch: clampToRange(*trackingStartPitch, *trackingPitchMin, *trackingPitchMax),
	}
	frameNumber := 0
	lastFPSAt := time.Now()
	fpsFrames := 0
	fps := 0.0

	for {
		if ok := capture.Read(&frame); !ok {
			return fmt.Errorf("capture source ended: %s", capture.Describe())
		}
		if frame.Empty() {
			continue
		}

		frameNumber++
		fpsFrames++
		elapsed := time.Since(lastFPSAt)
		if elapsed >= time.Second {
			fps = float64(fpsFrames) / elapsed.Seconds()
			fpsFrames = 0
			lastFPSAt = time.Now()
		}

		if frameNumber%resolvedProcessEvery == 0 {
			gocv.CvtColor(frame, &frameRGB, gocv.ColorBGRToRGB)
			imageFrame, err := frameRGB.ToImage()
			if err != nil {
				lastDetectErr = fmt.Sprintf("frame conversion failed: %v", err)
			} else {
				detectionFrame := cropFrameForDetection(imageFrame, resolvedCameraType)
				detections, detErr := detector.DetectHumans(detectionFrame)
				if detErr != nil {
					lastDetectErr = detErr.Error()
				} else {
					lastDetections = detections
					lastDetectErr = ""
				}
			}
		}

		drawDetections(&frame, lastDetections, green, black)

		target, targetPoint, targetType, found := selectTrackingTarget(lastDetections, mode, float32(resolvedPoseLeftHandMinConfidence))
		trackingViewport := detectionViewport(frame.Cols(), frame.Rows(), resolvedCameraType)
		controllerMode := "SCANNING"
		if found {
			controllerMode = "TRACKING"
		}

		// Log target selection periodically so tracking activity is visible in the terminal.
		if frameNumber%90 == 0 {
			logger.WithFields(logrus.Fields{
				"frame":      frameNumber,
				"detections": len(lastDetections),
				"found":      found,
				"mode":       mode,
			}).Info("VIEWER TRACK: target selection")
		}

		prevPan := trackingState.pan
		prevPitch := trackingState.pitch

		if resolvedTrackingDebug {
			drawTrackingDebugOverlay(
				&frame,
				target,
				targetPoint,
				targetType,
				found,
				controllerMode,
				mode,
				trackingViewport,
				&trackingState,
				resolvedTrackingDeadZone,
				resolvedTrackingPanSensitivity,
				resolvedTrackingPitchSensitivity,
				resolvedTrackingPanMin,
				resolvedTrackingPanMax,
				resolvedTrackingPitchMin,
				resolvedTrackingPitchMax,
				resolvedTrackingInvertPan,
				resolvedTrackingInvertTilt,
				red,
				black,
			)
		}

		panMoved := math.Abs(trackingState.pan-prevPan) > 0.001
		pitchMoved := math.Abs(trackingState.pitch-prevPitch) > 0.001
		if panMoved || pitchMoved {
			if resolvedControlURL != "" {
				logger.WithFields(logrus.Fields{
					"pan":        trackingState.pan,
					"panDelta":   trackingState.pan - prevPan,
					"pitch":      trackingState.pitch,
					"pitchDelta": trackingState.pitch - prevPitch,
				}).Info("VIEWER TRACK: sending axis commands")
				if panMoved {
					sendViewerAxisCommand(resolvedControlURL, "pan", trackingState.pan, logger)
				}
				if pitchMoved {
					sendViewerAxisCommand(resolvedControlURL, "tilt", trackingState.pitch, logger)
				}
			} else {
				logger.WithFields(logrus.Fields{
					"pan":        trackingState.pan,
					"panDelta":   trackingState.pan - prevPan,
					"pitch":      trackingState.pitch,
					"pitchDelta": trackingState.pitch - prevPitch,
				}).Info("VIEWER TRACK: dry-run (set --control-url to send real commands)")
			}
		}

		drawHUD(&frame, resolvedRPCEndpoint, capture.Describe(), frameNumber, fps, lastDetectErr, green, black)
		window.IMShow(frame)

		key := window.WaitKey(1)
		if key == 27 || key == int('q') {
			return nil
		}
	}
}

// Helper for extracting --profile and --config
func extractStringFlag(args []string, name string) (string, bool) {
	longName := "--" + name
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == longName {
			if i+1 >= len(args) {
				return "", true
			}
			return args[i+1], true
		}
		if strings.HasPrefix(arg, longName+"=") {
			return strings.TrimPrefix(arg, longName+"="), true
		}
	}
	return "", false
}

type trackingDebugState struct {
	pan   float64
	pitch float64
}

func selectTrackingTarget(detections []softrainsvision.Detection, mode string, minLeftWristConf float32) (softrainsvision.Detection, image.Point, string, bool) {
	bestIdx := -1
	bestScore := float32(-1)
	for i, d := range detections {
		if mode == "pose" && !softrainsvision.HasConfidentLeftHand(d.Pose, minLeftWristConf) {
			continue
		}
		if d.Confidence > bestScore {
			bestScore = d.Confidence
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return softrainsvision.Detection{}, image.Point{}, "none", false
	}
	target := detections[bestIdx]
	if mode == "pose" {
		if wrist, ok := target.Pose[softrainsvision.PoseKeyLeftWrist]; ok {
			box := target.BoundingBox
			x := box.Min.X + int(clamp01(wrist.X)*float32(box.Dx()))
			y := box.Min.Y + int(clamp01(wrist.Y)*float32(box.Dy()))
			return target, image.Pt(x, y), "left-wrist", true
		}
	}
	center := image.Pt(target.BoundingBox.Min.X+target.BoundingBox.Dx()/2, target.BoundingBox.Min.Y+target.BoundingBox.Dy()/2)
	return target, center, "bbox-center", true
}

func drawTrackingDebugOverlay(
	frame *gocv.Mat,
	target softrainsvision.Detection,
	targetPoint image.Point,
	targetType string,
	found bool,
	controllerMode string,
	mode string,
	trackingViewport image.Rectangle,
	state *trackingDebugState,
	deadZone int,
	panSensitivity float64,
	pitchSensitivity float64,
	panMin float64,
	panMax float64,
	pitchMin float64,
	pitchMax float64,
	invertPan bool,
	invertTilt bool,
	stroke color.RGBA,
	textBg color.RGBA,
) {
	// panSign: negate dx so that when the subject is right of center (dx>0)
	// the pan command goes negative (camera moves right to follow).
	// If the gimbal still steers the wrong way, pass --tracking-invert-pan.
	panSign := -1.0
	if invertPan {
		panSign = 1.0
	}
	tiltSign := 1.0 // subject below center (dy>0) -> positive tilt follows downward
	if invertTilt {
		tiltSign = -1.0
	}

	frameW := frame.Cols()
	frameH := frame.Rows()
	if trackingViewport.Empty() {
		trackingViewport = image.Rect(0, 0, frameW, frameH)
	}
	centerX := trackingViewport.Min.X + trackingViewport.Dx()/2
	centerY := trackingViewport.Min.Y + trackingViewport.Dy()/2

	// Draw center crosshair and deadzone for quick visual alignment.
	gocv.Line(frame, image.Pt(centerX-20, centerY), image.Pt(centerX+20, centerY), stroke, 1)
	gocv.Line(frame, image.Pt(centerX, centerY-20), image.Pt(centerX, centerY+20), stroke, 1)
	if deadZone > 0 {
		gocv.Rectangle(frame, imageRect(centerX-deadZone, centerY-deadZone, centerX+deadZone, centerY+deadZone), stroke, 1)
	}
	if trackingViewport.Dx() != frameW || trackingViewport.Dy() != frameH {
		gocv.Rectangle(frame, trackingViewport, stroke, 1)
	}

	panDelta := 0.0
	pitchDelta := 0.0
	dx := 0
	dy := 0
	if found {
		rect := target.BoundingBox
		gocv.Rectangle(frame, rect, stroke, 3)
		targetX := targetPoint.X
		targetY := targetPoint.Y
		dx = targetX - centerX
		dy = targetY - centerY
		gocv.Circle(frame, image.Pt(targetX, targetY), 5, stroke, 2)
		gocv.Line(frame, image.Pt(centerX, centerY), image.Pt(targetX, targetY), stroke, 1)

		if absInt(dx) > deadZone {
			panDelta = panSign * float64(dx) * panSensitivity
		}
		if absInt(dy) > deadZone {
			pitchDelta = tiltSign * float64(dy) * pitchSensitivity
		}

		state.pan = clampToRange(state.pan+panDelta, panMin, panMax)
		state.pitch = clampToRange(state.pitch+pitchDelta, pitchMin, pitchMax)
	}

	lines := []string{
		fmt.Sprintf("Controller mode: %s", controllerMode),
		fmt.Sprintf("Tracking mode: %s", mode),
		fmt.Sprintf("Target type: %s", targetType),
		fmt.Sprintf("Active target: %t", found),
		fmt.Sprintf("Center delta px: dx=%d dy=%d", dx, dy),
		fmt.Sprintf("Required adj: pan=%.2f pitch=%.2f", panDelta, pitchDelta),
		fmt.Sprintf("Suggested pan/pitch: %.2f / %.2f (pan %.0f..%.0f, tilt %.0f..%.0f)", state.pan, state.pitch, panMin, panMax, pitchMin, pitchMax),
	}
	if found {
		lines = append(lines, fmt.Sprintf("Target confidence: %.2f", target.Confidence))
	}

	y := frameH - (len(lines) * 22) - 12
	if y < 18 {
		y = 18
	}
	for _, line := range lines {
		textRect := imageRect(8, y-16, 620, y+4)
		gocv.Rectangle(frame, textRect, textBg, -1)
		gocv.PutText(frame, line, imagePoint(12, y), gocv.FontHersheySimplex, 0.55, stroke, 1)
		y += 22
	}
}

func normalizedCameraType(cameraType string) string {
	switch strings.ToLower(strings.TrimSpace(cameraType)) {
	case "stereo", "stereo-left", "stereo_side_by_side", "stereo-side-by-side", "dual-left":
		return "stereo-left"
	default:
		return "standard"
	}
}

func detectionViewport(frameWidth, frameHeight int, cameraType string) image.Rectangle {
	switch normalizedCameraType(cameraType) {
	case "stereo-left":
		return image.Rect(0, 0, frameWidth/2, frameHeight)
	default:
		return image.Rect(0, 0, frameWidth, frameHeight)
	}
}

func cropFrameForDetection(frame image.Image, cameraType string) image.Image {
	viewport := detectionViewport(frame.Bounds().Dx(), frame.Bounds().Dy(), cameraType)
	if viewport.Dx() == frame.Bounds().Dx() && viewport.Dy() == frame.Bounds().Dy() {
		return frame
	}
	cropped := image.NewRGBA(image.Rect(0, 0, viewport.Dx(), viewport.Dy()))
	draw.Draw(cropped, cropped.Bounds(), frame, viewport.Min, draw.Src)
	return cropped
}

func sendViewerAxisCommand(baseURL, axis string, position float64, logger *logrus.Logger) {
	posStr := strconv.FormatFloat(position, 'f', 2, 64)
	commandURL := fmt.Sprintf("%s/%s?position=%s", baseURL, axis, url.QueryEscape(posStr))
	req, err := http.NewRequest("PUT", commandURL, nil)
	if err != nil {
		logger.WithError(err).Errorf("VIEWER TRACK: failed to build %s request", axis)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.WithFields(logrus.Fields{"axis": axis, "url": commandURL, "error": err}).Error("VIEWER TRACK: HTTP call failed")
		return
	}
	defer resp.Body.Close()
	logger.WithFields(logrus.Fields{
		"axis":     axis,
		"position": position,
		"status":   resp.StatusCode,
		"url":      commandURL,
	}).Infof("VIEWER TRACK: %s -> %.2f (HTTP %d)", axis, position, resp.StatusCode)
}

func clampToRange(v, minV, maxV float64) float64 {
	if minV > maxV {
		minV, maxV = maxV, minV
	}
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func drawDetections(frame *gocv.Mat, detections []softrainsvision.Detection, stroke color.RGBA, textBg color.RGBA) {
	frameW := frame.Cols()
	frameH := frame.Rows()
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
		for _, name := range poseKeypointOrder {
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

			kpLabel := name
			if short, exists := poseKeypointShortName[name]; exists {
				kpLabel = short
			}
			labelPt := image.Pt(px+5, py-5)
			labelBg := imageRect(labelPt.X-2, labelPt.Y-11, labelPt.X+28, labelPt.Y+3)
			gocv.Rectangle(frame, labelBg, textBg, -1)
			gocv.PutText(frame, kpLabel, labelPt, gocv.FontHersheySimplex, 0.35, poseColor, 1)
		}
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

func drawHUD(frame *gocv.Mat, endpoint, source string, frameNumber int, fps float64, lastErr string, stroke color.RGBA, textBg color.RGBA) {
	lines := []string{
		fmt.Sprintf("RPC: %s", endpoint),
		fmt.Sprintf("Source: %s", source),
		fmt.Sprintf("Frame: %d", frameNumber),
		fmt.Sprintf("FPS: %.1f", fps),
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

func imageRect(minX, minY, maxX, maxY int) image.Rectangle {
	return image.Rect(minX, minY, maxX, maxY)
}

func imagePoint(x, y int) image.Point {
	return image.Pt(x, y)
}
```
