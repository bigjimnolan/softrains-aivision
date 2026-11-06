package softrainsvision

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"image"
	"net"
	"os"
	"softrains-detector/pkg/softrainsrpc"
	"softrains-detector/pkg/startup"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/image/draw"
)

type rpcKeypoint struct {
	X          float32 `json:"x"`
	Y          float32 `json:"y"`
	Confidence float32 `json:"confidence"`
}

type rpcPose map[string]rpcKeypoint

type rpcResponse struct {
	Detections []struct {
		ClassID    int      `json:"class_id"`
		Confidence float32  `json:"confidence"`
		X          float32  `json:"x"`
		Y          float32  `json:"y"`
		Width      float32  `json:"width"`
		Height     float32  `json:"height"`
		Pose       *rpcPose `json:"pose"`
	} `json:"detections"`
	Count         int    `json:"count"`
	PoseEnabled   bool   `json:"pose_enabled"`
	PoseRequested bool   `json:"pose_requested"`
	Error         string `json:"error"`
}

type RPCDetector struct {
	address             string
	serverName          string
	confidenceThreshold float32
	nmsThreshold        float32
	maxAreaRatio        float32
	maxWidthRatio       float32
	maxHeightRatio      float32
	minBoxWidth         int
	minBoxHeight        int
	minBoxArea          int
	colorAnalysis       startup.ColorAnalysisConfig
	requestTimeout      time.Duration
	logger              *logrus.Logger
	tlsConfig           *tls.Config
	cameraType          startup.CameraType
	trackPose           bool
	poseClient          *softrainsrpc.PoseClient
}

func DetectionViewport(frameWidth, frameHeight int, cameraType startup.CameraType) image.Rectangle {
	// For fisheye cameras, we can only trust the central circular region where distortion is minimal.
	if cameraType == startup.CameraTypeFisheyeStereoLeft {
		radius := int(float64(frameWidth) * 0.45)
		centerX := frameWidth / 2
		centerY := frameHeight / 2
		return image.Rect(centerX-radius, centerY-radius, centerX+radius, centerY+radius)
	}
	// For stereo camers, we can restrict to the left half of the frame to avoid duplicate detections of the same person.
	if cameraType == startup.CameraTypeStereo {
		return image.Rect(0, 0, frameWidth/2, frameHeight)
	}
	// For normal cameras, use the full frame.
	return image.Rect(0, 0, frameWidth, frameHeight)
}

func NewHumanDetectorRPC(
	aiConfig startup.AIConfig,
	logger *logrus.Logger,
) (*RPCDetector, error) {
	if aiConfig.RPCEndpoint == "" {
		return nil, fmt.Errorf("rpc endpoint address is required")
	}

	if aiConfig.ServerName == "" {
		return nil, fmt.Errorf("server name is required for TLS connection")
	}

	rootCAs, err := x509.SystemCertPool()
	if err != nil || rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	if aiConfig.CACert != "" {
		pemBytes, readErr := os.ReadFile(aiConfig.CACert)
		if readErr != nil {
			return nil, fmt.Errorf("failed to read CA certificate %s: %w", aiConfig.CACert, readErr)
		}
		if ok := rootCAs.AppendCertsFromPEM(pemBytes); !ok {
			return nil, fmt.Errorf("failed to parse CA certificate %s", aiConfig.CACert)
		}
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		ServerName: aiConfig.ServerName,
		RootCAs:    rootCAs,
	}

	detector := &RPCDetector{
		address:             aiConfig.RPCEndpoint,
		serverName:          aiConfig.ServerName,
		confidenceThreshold: aiConfig.ConfidenceThreshold,
		nmsThreshold:        aiConfig.NMSThreshold,
		minBoxWidth:         aiConfig.DetectionFilters.MinBoxWidth,
		minBoxHeight:        aiConfig.DetectionFilters.MinBoxHeight,
		minBoxArea:          aiConfig.DetectionFilters.MinBoxArea,
		maxAreaRatio:        aiConfig.DetectionFilters.MaxAreaRatio,
		maxWidthRatio:       aiConfig.DetectionFilters.MaxWidthRatio,
		maxHeightRatio:      aiConfig.DetectionFilters.MaxHeightRatio,
		colorAnalysis:       aiConfig.ColorAnalysis,
		requestTimeout:      10 * time.Second,
		logger:              logger,
		tlsConfig:           tlsConfig,
		trackPose:           aiConfig.DetectionFilters.UsePose,
	}

	poseClient, err := softrainsrpc.NewPoseClient(
		aiConfig.RPCEndpoint,
		aiConfig.ServerName,
		aiConfig.CACert,
		detector.requestTimeout,
	)
	if err != nil {
		return nil, fmt.Errorf("initialize shared pose rpc client: %w", err)
	}
	detector.poseClient = poseClient

	if err := detector.verifyConnection(); err != nil {
		return nil, err
	}

	logger.WithFields(logrus.Fields{
		"endpoint":   aiConfig.RPCEndpoint,
		"serverName": aiConfig.ServerName,
	}).Info("Connected to RPC detector endpoint")

	return detector, nil
}

func (hd *RPCDetector) verifyConnection() error {
	conn, err := hd.dial()
	if err != nil {
		return fmt.Errorf("cannot connect to rpc detector at %s: %w", hd.address, err)
	}
	return conn.Close()
}

func (hd *RPCDetector) dial() (*tls.Conn, error) {
	dialer := &net.Dialer{Timeout: hd.requestTimeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", hd.address, hd.tlsConfig)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(hd.requestTimeout)
	if err := conn.SetDeadline(deadline); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func (hd *RPCDetector) DetectHumans(frame image.Image) ([]Detection, error) {
	frameWidth := frame.Bounds().Dx()
	frameHeight := frame.Bounds().Dy()

	resized := image.NewRGBA(image.Rect(0, 0, 300, 300))
	draw.NearestNeighbor.Scale(resized, resized.Bounds(), frame, frame.Bounds(), draw.Over, nil)

	rgbBytes := rawRGBFromImage(resized)
	response, err := hd.sendDetectionRequest(300, 300, rgbBytes)
	if err != nil {
		return nil, fmt.Errorf("rpc detector request failed: %w", err)
	}

	if response.Error != "" {
		return []Detection{}, fmt.Errorf("rpc detector returned error: %s", response.Error)
	}

	return hd.parseDetections(response, frameWidth, frameHeight), nil
}

func (hd *RPCDetector) sendDetectionRequest(width, height int, rgbBytes []byte) (*rpcResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), hd.requestTimeout)
	defer cancel()

	env, err := hd.poseClient.DetectRGB24(ctx, width, height, rgbBytes, hd.trackPose, "")
	if err != nil {
		return nil, err
	}
	if !env.Success {
		return nil, fmt.Errorf("rpc detector returned error: %s", env.Error)
	}
	if len(env.Result) == 0 {
		return nil, fmt.Errorf("rpc detector returned empty result")
	}

	var response rpcResponse
	if err := json.Unmarshal(env.Result, &response); err != nil {
		return nil, fmt.Errorf("failed to parse rpc response: %w", err)
	}

	return &response, nil
}

func (hd *RPCDetector) parseDetections(response *rpcResponse, frameWidth, frameHeight int) []Detection {
	var detections []Detection
	criteria := HumanDetectionCriteria{
		ConfidenceThreshold: hd.confidenceThreshold,
		MinBoxWidth:         hd.minBoxWidth,
		MinBoxHeight:        hd.minBoxHeight,
		MinBoxArea:          hd.minBoxArea,
		MaxAreaRatio:        hd.maxAreaRatio,
		MaxWidthRatio:       hd.maxWidthRatio,
		MaxHeightRatio:      hd.maxHeightRatio,
	}

	for _, det := range response.Detections {
		detection, ok := BuildHumanDetection(
			det.Confidence,
			det.X,
			det.Y,
			det.Width,
			det.Height,
			frameWidth,
			frameHeight,
			criteria,
		)
		if !ok {
			continue
		}

		if det.Pose != nil {
			pose := make(map[string]Keypoint, len(*det.Pose))
			for name, kp := range *det.Pose {
				pose[name] = Keypoint{
					X:          kp.X,
					Y:          kp.Y,
					Confidence: kp.Confidence,
				}
			}
			AttachPoseToDetection(&detection, pose)
		}
		detections = append(detections, detection)
	}

	return detections
}

func (hd *RPCDetector) Close() error {
	hd.logger.Info("Closed RPC detector client")
	return nil
}

func (hd *RPCDetector) cropFrameForDetection(frame image.Image, cameraType startup.CameraType) image.Image {
	viewport := DetectionViewport(frame.Bounds().Dx(), frame.Bounds().Dy(), startup.CameraType(cameraType))
	if viewport.Dx() == frame.Bounds().Dx() && viewport.Dy() == frame.Bounds().Dy() {
		return frame
	}
	cropped := image.NewRGBA(image.Rect(0, 0, viewport.Dx(), viewport.Dy()))
	draw.Draw(cropped, cropped.Bounds(), frame, viewport.Min, draw.Src)
	return cropped
}

func (hd *RPCDetector) ProcessFrame(imageFrame image.Image) (Detection, image.Point, string, error) {
	detectionFrame := hd.cropFrameForDetection(imageFrame, hd.cameraType)
	detections, err := hd.DetectHumans(detectionFrame)
	if err != nil {
		return Detection{}, image.Point{}, "none", nil
	}

	bestIdx := -1
	bestScore := float32(-1)
	for i, d := range detections {
		if d.Confidence > bestScore {
			bestScore = d.Confidence
			bestIdx = i
		}
	}

	if bestIdx < 0 {
		return Detection{}, image.Point{}, "none", nil
	}

	target := detections[bestIdx]
	if hd.colorAnalysis.Enabled {
		mode := hd.colorAnalysis.Mode
		if mode == "" {
			mode = ColorModeDominant
		}
		if colorName, ok := AnalyzeDetectionColor(detectionFrame, target.BoundingBox, mode, hd.colorAnalysis.SampleStep); ok {
			target.DominantColor = colorName
			target.ColorMode = mode
		}
	}

	if hd.trackPose {
		if wrist, ok := target.Pose[PoseKeyLeftWrist]; ok {
			normalizedWristCenter := image.Point{
				X: target.BoundingBox.Min.X + int(wrist.X*float32(target.BoundingBox.Dx())),
				Y: target.BoundingBox.Min.Y + int(wrist.Y*float32(target.BoundingBox.Dy())),
			}
			return target, normalizedWristCenter, "left-wrist", nil
		}
		hd.logger.Debug("Pose tracking enabled but left wrist keypoint not found")
	}

	hd.logger.Debug("Pose tracking disabled, using bounding box center")
	center := image.Pt(target.BoundingBox.Min.X+target.BoundingBox.Dx()/2, target.BoundingBox.Min.Y+target.BoundingBox.Dy()/2)
	return target, center, "bbox-center", nil
}

// rawRGBFromImage converts an image.Image to raw RGB bytes (24-bit RGB)
func rawRGBFromImage(img image.Image) []byte {
	bounds := img.Bounds()
	width := bounds.Max.X - bounds.Min.X
	height := bounds.Max.Y - bounds.Min.Y

	rgb := make([]byte, width*height*3)
	idx := 0

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			// RGBA returns 16-bit values, convert to 8-bit
			rgb[idx] = byte(r >> 8)
			rgb[idx+1] = byte(g >> 8)
			rgb[idx+2] = byte(b >> 8)
			idx += 3
		}
	}

	return rgb
}
