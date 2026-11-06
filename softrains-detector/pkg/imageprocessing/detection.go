package imageprocessing

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"softrains-detector/internal/utils"
	"strings"
	"syscall"
	"time"

	"softrains-detector/pkg/softrainsvision"
	"softrains-detector/pkg/startup"

	"github.com/sirupsen/logrus"
)

// DetectionOnlyService runs a headless detection loop by polling JPEG snapshots.
// This avoids coupling capture+render in one process and is useful for low-latency
// detection pipelines where the viewer runs separately.
type DetectionOnlyService struct {
	logger         *logrus.Logger
	startupProfile startup.StartupProfile
	detector       softrainsvision.RPCDetector
	actionChannel  chan<- softrainsvision.DetectionReturn
	viewerChannel  chan softrainsvision.DetectionReturn
	panProvider    softrainsvision.PanProvider
	snapshotURL    string
	pollInterval   time.Duration
	httpClient     *http.Client
}

type snapshotRPCRequest struct{}

type snapshotRPCReply struct {
	Ok          bool
	Message     string
	ContentType string
	JPEG        []byte
}

func NewDetectionOnlyService(
	startupProfile startup.StartupProfile,
	logger *logrus.Logger,
	actionChannel chan<- softrainsvision.DetectionReturn,
	viewerChannel chan softrainsvision.DetectionReturn,
	panProvider softrainsvision.PanProvider,
	snapshotURL string,
	pollInterval time.Duration,
	requestTimeout time.Duration,
) (*DetectionOnlyService, error) {
	if snapshotURL == "" {
		return nil, fmt.Errorf("snapshot URL is required")
	}
	if pollInterval <= 0 {
		pollInterval = 120 * time.Millisecond
	}
	if requestTimeout <= 0 {
		requestTimeout = 1 * time.Second
	}

	hd, err := softrainsvision.NewHumanDetectorRPC(
		startupProfile.BotSetup.AI,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize detector: %w", err)
	}

	return &DetectionOnlyService{
		logger:         logger,
		startupProfile: startupProfile,
		detector:       *hd,
		actionChannel:  actionChannel,
		viewerChannel:  viewerChannel,
		panProvider:    panProvider,
		snapshotURL:    snapshotURL,
		pollInterval:   pollInterval,
		httpClient: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				DisableKeepAlives:   false,
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     30 * time.Second,
			},
		},
	}, nil
}

func (ds *DetectionOnlyService) Start(ctx context.Context) error {
	processEvery := ds.startupProfile.BotSetup.AI.ProcessEveryNFrames
	if processEvery <= 0 {
		processEvery = 1
	}

	ticker := time.NewTicker(ds.pollInterval)
	defer ticker.Stop()

	frameNumber := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			frameNumber++
			if frameNumber%processEvery != 0 {
				continue
			}

			img, err := ds.fetchSnapshot(ctx)
			if err != nil {
				ds.logger.WithError(err).Warn("snapshot fetch failed")
				continue
			}

			img = applyOrientation(img, ds.startupProfile.BotSetup.Connection.Camera)

			detection, detectionCenter, detectionType, err := ds.detector.ProcessFrame(img)
			if err != nil {
				ds.logger.WithError(err).Warn("detection failed")
				continue
			}

			// send on actions channel and viewer channel for display
			if ds.startupProfile.Detector.Enabled != nil && *ds.startupProfile.Detector.Enabled == true {
				select {
				case ds.viewerChannel <- softrainsvision.DetectionReturn{
					DetectionPoint: detectionCenter,
					PanAtDetection: ds.panProvider.GetPan(),
					Image:          img,
					Detections:     []softrainsvision.Detection{detection},
				}:
					ds.logger.WithFields(logrus.Fields{
						"confidence": detection.Confidence,
						"targetType": detectionType,
						"centerX":    detectionCenter.X,
						"centerY":    detectionCenter.Y,
					}).Debug("sent detection to viewer channel")
				default:
					ds.logger.Warn("viewer channel is full; dropping frame")
				}
			}

			// if we have a valid detection, send it to the action channel for tracking
			if detectionType != "none" {
				select {
				case ds.actionChannel <- softrainsvision.DetectionReturn{
					DetectionPoint: detectionCenter,
					PanAtDetection: ds.panProvider.GetPan(),
					Image:          img,
				}:
					ds.logger.WithFields(logrus.Fields{
						"confidence": detection.Confidence,
						"targetType": detectionType,
						"centerX":    detectionCenter.X,
						"centerY":    detectionCenter.Y,
					}).Debug("published detection")
				default:
					ds.logger.Warn("action channel is full; dropping detection")
				}
			}
		}
	}
}

func (ds *DetectionOnlyService) fetchSnapshot(ctx context.Context) (image.Image, error) {
	control := ds.startupProfile.BotSetup.Connection.Control
	if control.RPCEndpoint == "" {
		return nil, fmt.Errorf("control rpc endpoint is not configured")
	}
	if control.ServerName == "" {
		return nil, fmt.Errorf("control rpc serverName is not configured")
	}

	rootCAs, err := x509.SystemCertPool()
	if err != nil || rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if control.CACert != "" {
		pemBytes, readErr := os.ReadFile(control.CACert)
		if readErr != nil {
			return nil, fmt.Errorf("read control CA certificate %s: %w", control.CACert, readErr)
		}
		if ok := rootCAs.AppendCertsFromPEM(pemBytes); !ok {
			return nil, fmt.Errorf("parse control CA certificate %s", control.CACert)
		}
	}

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		ServerName: control.ServerName,
		RootCAs:    rootCAs,
	}

	timeout := ds.httpClient.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	retryBudget := time.Duration(ds.startupProfile.BotSetup.Tracking.ControlTimeoutMs) * time.Millisecond
	retryInterval := time.Duration(ds.startupProfile.BotSetup.Tracking.ControlRetryIntervalMs) * time.Millisecond

	var img image.Image
	err = utils.Retry(
		ctx,
		retryBudget,
		retryInterval,
		isRetryableSnapshotRPCError,
		func() error {
			frame, fetchErr := fetchSnapshotRPCOnce(ctx, control, tlsCfg, timeout)
			if fetchErr != nil {
				return fetchErr
			}
			img = frame
			return nil
		},
	)
	if err != nil {
		return nil, err
	}

	return img, nil
}

func fetchSnapshotRPCOnce(ctx context.Context, control startup.BotControlConfig, tlsCfg *tls.Config, timeout time.Duration) (image.Image, error) {
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", control.RPCEndpoint, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("dial snapshot rpc endpoint %s: %w", control.RPCEndpoint, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("set rpc deadline: %w", err)
		}
	} else {
		if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
			return nil, fmt.Errorf("set rpc deadline: %w", err)
		}
	}

	rpcClient := rpc.NewClient(conn)
	defer rpcClient.Close()

	var reply snapshotRPCReply
	if err := rpcClient.Call("GimbalRPC.GetSnapshotJPEG", snapshotRPCRequest{}, &reply); err != nil {
		return nil, fmt.Errorf("call GimbalRPC.GetSnapshotJPEG: %w", err)
	}
	if !reply.Ok {
		if reply.Message == "" {
			return nil, fmt.Errorf("snapshot rpc returned not-ok response")
		}
		return nil, fmt.Errorf("snapshot rpc returned not-ok response: %s", reply.Message)
	}
	if len(reply.JPEG) == 0 {
		return nil, fmt.Errorf("snapshot rpc returned empty jpeg payload")
	}
	if reply.ContentType != "" {
		contentType := strings.ToLower(reply.ContentType)
		if !strings.Contains(contentType, "jpeg") && !strings.Contains(contentType, "jpg") {
			return nil, fmt.Errorf("snapshot rpc returned unsupported content type %q", reply.ContentType)
		}
	}

	img, _, err := image.Decode(bytes.NewReader(reply.JPEG))
	if err != nil {
		return nil, fmt.Errorf("decode snapshot rpc jpeg: %w", err)
	}
	return img, nil
}

func isRetryableSnapshotRPCError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	if netErr, ok := err.(net.Error); ok {
		if netErr.Timeout() {
			return true
		}
		if temporary, ok := interface{}(netErr).(interface{ Temporary() bool }); ok && temporary.Temporary() {
			return true
		}
	}
	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "connection refused") ||
		strings.Contains(errText, "unexpected eof") ||
		strings.Contains(errText, "connection reset by peer") ||
		strings.Contains(errText, "broken pipe")
}

func applyOrientation(src image.Image, cam *startup.CameraConfig) image.Image {
	if cam == nil || cam.Orientation == nil {
		return src
	}

	img := src
	rotate := cam.Orientation.Rotate
	switch rotate {
	case 90:
		img = rotate90CW(img)
	case 180:
		img = rotate180(img)
	case 270:
		img = rotate90CCW(img)
	}

	if cam.Orientation.FlipHorizontal {
		img = flipHorizontal(img)
	}
	if cam.Orientation.FlipVertical {
		img = flipVertical(img)
	}

	return img
}

func rotate90CW(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(b.Max.Y-1-y, x-b.Min.X, src.At(x, y))
		}
	}
	return dst
}

func rotate90CCW(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(y-b.Min.Y, b.Max.X-1-x, src.At(x, y))
		}
	}
	return dst
}

func rotate180(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(b.Max.X-1-x, b.Max.Y-1-y, src.At(x, y))
		}
	}
	return dst
}

func flipHorizontal(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(b.Max.X-1-x, y-b.Min.Y, src.At(x, y))
		}
	}
	return dst
}

func flipVertical(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(x-b.Min.X, b.Max.Y-1-y, src.At(x, y))
		}
	}
	return dst
}
