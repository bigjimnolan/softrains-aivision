package softrainsvision

import (
	"errors"
	"image"
	"math"
	"net"
	"net/http"
	"softrains-detector/pkg/startup"

	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Actioneer consumes parsed tracking snapshots and performs all camera/laser actions.
type Actioneer struct {
	frameWidth       int
	frameHeight      int
	trackingViewport image.Rectangle

	controlURL      string
	controlProtocol string

	currentPan  float64
	currentTilt float64
	currentRoll float64

	panMin  float64
	panMax  float64
	tiltMin float64
	tiltMax float64
	rollMin float64
	rollMax float64

	fieldOfView float64 // in degrees, horizontal FOV of the camera; used to calculate pan/tilt adjustments based on pixel deltas
	deadZone    int

	markingMode bool

	scanEnabled   bool
	scanRange     int
	scanStep      float64
	scanInterval  time.Duration
	scanDirection int
	scanCenter    float64

	laserEnabled         bool
	laserCurrentState    bool
	commandTimeout       time.Duration
	commandRetryInterval time.Duration

	actionTriggers        []startup.ActionTrigger
	activeTrackingTrigger startup.ActionTriggerType

	logger *logrus.Logger

	httpClient *http.Client
	rpcClient  *RPCControlClient

	lastSnapshotMu        sync.RWMutex
	lastProcessedSnapshot TrackerSnapshot
	switchStateMu         sync.Mutex
	switchState           map[int]bool
	switchStateKnown      map[int]bool

	axisInFlight map[string]bool

	lastDetection      time.Time
	lastHumanDetection time.Time
	detectionTimeout   time.Duration
	detectionChan      chan DetectionReturn
	viewerChan         chan DetectionReturn

	speechMu       sync.Mutex
	speechInFlight *bool
}

func NewActioneer(config startup.StartupProfile, logger *logrus.Logger) *Actioneer {
	actioneer := Actioneer{
		frameWidth:       config.BotSetup.Connection.Camera.Width,
		frameHeight:      config.BotSetup.Connection.Camera.Height,
		trackingViewport: image.Rect(0, 0, config.BotSetup.Connection.Camera.Width, config.BotSetup.Connection.Camera.Height),

		controlURL:      config.BotSetup.Connection.Control.URL,
		controlProtocol: config.BotSetup.Connection.Control.Protocol,

		currentPan:  90,
		currentTilt: 90,
		currentRoll: 90,
		fieldOfView: config.BotSetup.Connection.Camera.FOV,
		panMin:      config.BotSetup.Connection.Control.Pan.Min,
		panMax:      config.BotSetup.Connection.Control.Pan.Max,
		tiltMin:     config.BotSetup.Connection.Control.Tilt.Min,
		tiltMax:     config.BotSetup.Connection.Control.Tilt.Max,
		rollMin:     config.BotSetup.Connection.Control.Roll.Min,
		rollMax:     config.BotSetup.Connection.Control.Roll.Max,
		deadZone:    config.BotSetup.Tracking.DeadZone,

		scanEnabled:          config.BotSetup.Tracking.ScanMode.Enabled,
		scanRange:            int(config.BotSetup.Connection.Control.Pan.Max),
		scanStep:             float64(config.BotSetup.Tracking.ScanMode.ScanStep),
		scanInterval:         time.Duration(config.BotSetup.Tracking.ScanMode.ScanIntervalMs) * time.Millisecond,
		scanDirection:        1,
		scanCenter:           config.BotSetup.Connection.Control.Pan.Center,
		laserEnabled:         config.BotSetup.Tracking.Laser.Enabled,
		laserCurrentState:    true,
		commandTimeout:       time.Duration(config.BotSetup.Tracking.ControlTimeoutMs) * time.Millisecond,
		commandRetryInterval: time.Duration(config.BotSetup.Tracking.ControlRetryIntervalMs) * time.Millisecond,
		detectionTimeout:     time.Duration(config.BotSetup.Tracking.DetectionTimeoutMs) * time.Millisecond,

		logger:           logger,
		axisInFlight:     map[string]bool{},
		switchState:      map[int]bool{},
		switchStateKnown: map[int]bool{},
		detectionChan:    make(chan DetectionReturn, 10), // buffered channel to avoid blocking
		viewerChan:       make(chan DetectionReturn, 10), // buffered channel to avoid blocking
		lastDetection:    time.Now(),
		speechInFlight:   new(bool),
	}

	if actioneer.commandTimeout <= 0 {
		actioneer.commandTimeout = 30 * time.Second
	}
	if actioneer.commandRetryInterval <= 0 {
		actioneer.commandRetryInterval = 2 * time.Second
	}
	if actioneer.detectionTimeout <= 0 {
		actioneer.detectionTimeout = 2 * time.Minute
	}
	logger.Infof("Actioneer initialized with config: %+v\n", actioneer.commandTimeout) //
	rpcClient, err := NewRPCControlClient(config.BotSetup.Connection.Control, actioneer.commandTimeout, actioneer.commandRetryInterval)
	if err != nil {
		logger.WithError(err).Error("Failed to initialize TLS RPC control client")
	} else {
		actioneer.rpcClient = rpcClient
	}

	return &actioneer
}

func (a *Actioneer) Start() {
	for {
		// if I saw something recently, add more wait time
		wait := a.scanInterval
		if a.scanInterval > time.Since(a.lastDetection) {
			wait = a.scanInterval + time.Since(a.lastDetection)
		}

		select {
		case targetPoint, ok := <-a.detectionChan:
			if !ok {
				a.logger.Info("Detection channel closed, stopping Actioneer")
				return
			}
			a.logger.WithField("target", targetPoint).Info("Received new target point from detector")
			err := a.trackOnPoint(targetPoint)
			if err != nil {
				a.logger.WithError(err).Error("Failed to track on target point")
			}
			a.lastDetection = time.Now()
		case <-time.After(wait):
			if a.scanEnabled && time.Since(a.lastDetection) >= a.scanInterval {
				a.nextScan()
				a.lastDetection = time.Now()
			}
		}
	}
}

func (a *Actioneer) GetInputChannel() chan<- DetectionReturn {
	return a.detectionChan
}

func (a *Actioneer) GetViewerChannel() chan DetectionReturn {
	return a.viewerChan
}

func (a *Actioneer) nextScan() error {
	// no targets, keep moving.
	a.logger.Debugf("Back to scan at: %v", a.currentPan)

	if !a.lastHumanDetection.IsZero() && time.Since(a.lastHumanDetection) >= a.detectionTimeout {
		*a.speechInFlight = false // reset speech flag only after detectionTimeout expires
	}

	nextPan := a.currentPan + a.scanStep*float64(a.scanDirection)
	switchCommands := map[int]bool{
		1: false,  // blue
		2: true,  // scan mode laser
		3: false, // green
		4: true, // red
	}
	for id, state := range switchCommands {
		err := a.sendSwitchCommand(id, state)
		if err != nil {
			a.logger.WithError(err).Errorf("Failed to set switch %d to state %v", id, state)
		}
	}

	if nextPan >= a.panMax {
		nextPan = a.panMax
		a.scanDirection = -1
	} else if nextPan <= a.panMin {
		nextPan = a.panMin
		a.scanDirection = 1
	}

	return a.sendAxisCommand("pan", nextPan)
}

func (a *Actioneer) trackOnPoint(targetPoint DetectionReturn) error {
	// We see something, dim the lights
	//turn off scan mode laser if it was on
	lightMap := map[int]bool{
		1: true, // blue
		2: false, // scan mode laser
		3: false,  // green
		4: true, // red
	}

	for i := range lightMap {
		err := a.sendSwitchCommand(i, lightMap[i])
		if err != nil {
			a.logger.WithError(err).Errorf("Failed to set switch %d to state %v", i, lightMap[i])
		}
	}
	// Reset alert speech only after detectionTimeout has elapsed without any detections,
	// and refresh the timeout clock on every subsequent detection.
	if !a.lastHumanDetection.IsZero() && time.Since(a.lastHumanDetection) >= a.detectionTimeout {
		*a.speechInFlight = false
	}
	a.lastHumanDetection = time.Now()
	if !*a.speechInFlight {
		*a.speechInFlight = true
		err := a.rpcClient.SetSpeech("Hu Hu Hu Human detected. Have No No No Fear Fear No Fear None at all")
		if err != nil {
			return err
		}
	}

	centerX, centerY := a.trackingCenter()
	deltaX := targetPoint.DetectionPoint.X - centerX
	deltaY := targetPoint.DetectionPoint.Y - centerY

	if math.Abs(float64(deltaX)) < float64(a.deadZone) && math.Abs(float64(deltaY)) < float64(a.deadZone) {
		a.logger.Debugf("Target within dead zone (deltaX: %d, deltaY: %d), not adjusting", deltaX, deltaY)
		return nil
	}

	// Calculate pan and tilt adjustments with FOV support
	var panAdjustment, tiltAdjustment float64

	if a.fieldOfView > 0 {
		// FOV-aware calculation: degrees per pixel based on viewport dimensions
		viewportWidth := float64(a.trackingViewport.Dx())
		viewportHeight := float64(a.trackingViewport.Dy())
		if viewportWidth <= 0 {
			viewportWidth = float64(a.frameWidth)
		}
		if viewportHeight <= 0 {
			viewportHeight = float64(a.frameHeight)
		}

		// Pan: horizontal field of view maps to horizontal pixels
		degreesPerPixelPan := a.fieldOfView / viewportWidth
		panAdjustment = float64(deltaX) * degreesPerPixelPan
		// Tilt: vertical field of view maps to vertical pixels
		// Assuming 4:3 aspect ratio for simplicity, or use panSensitivity as scaling factor
		degreesPerPixelTilt := a.fieldOfView / viewportHeight
		tiltAdjustment = float64(deltaY) * degreesPerPixelTilt
	}
	a.logger.Debugf("Calculated pan adjustment: %.2f, tilt adjustment: %.2f (deltaX: %d, deltaY: %d)", panAdjustment, tiltAdjustment, deltaX, deltaY)
	desiredPan := targetPoint.PanAtDetection - panAdjustment
	//desiredTilt := a.currentTilt + tiltAdjustment
	axisCommands := map[string]float64{
		"pan": desiredPan,
		//"tilt": desiredTilt, TODO: get in config
	}
	for axis, position := range axisCommands {
		err := a.sendAxisCommand(axis, position)
		if err != nil {
			return err
		}
		a.logger.Tracef("Set %s to %.2f based on target pan at detection %.2f", axis, position, targetPoint.PanAtDetection)
	}
	return nil
}

func (a *Actioneer) shouldAnnounceTrackingTransition(lightMap map[int]bool) bool {
	a.switchStateMu.Lock()
	defer a.switchStateMu.Unlock()

	for id, desiredState := range lightMap {
		if !a.switchStateKnown[id] {
			return true
		}
		if a.switchState[id] != desiredState {
			return true
		}
	}

	return false
}

func (a *Actioneer) sendAxisCommand(axis string, position float64) error {
	if a.rpcClient == nil {
		a.logger.WithFields(logrus.Fields{"axis": axis}).Error("TLS RPC control client is not initialized")
		return errors.New("TLS RPC control client is not initialized")
	}
	if err := a.rpcClient.SetAxis(axis, position); err != nil {
		if isTimeoutError(err) {
			a.logger.WithFields(logrus.Fields{"axis": axis, "error": err}).Warn(
				"Camera command timed out awaiting RPC response; command may still be applied by device",
			)
		} else {
			a.logger.WithFields(logrus.Fields{"axis": axis, "error": err}).Error("Failed to send camera command via TLS RPC")
		}
		return err
	}

	// make rpc get state call to get current position and update currentPan/currentTilt/currentRoll
	resp, err := a.rpcClient.GetState()
	if err != nil {
		a.logger.WithFields(logrus.Fields{"axis": axis, "error": err}).Warn("Failed to refresh camera state via TLS RPC after axis command")
		return nil
	}

	a.currentPan = resp.State.Pan
	a.currentTilt = resp.State.Tilt
	a.currentRoll = resp.State.Roll
	return nil
}

func (a *Actioneer) sendSwitchCommand(id int, state bool) error {
	if a.rpcClient == nil {
		return errors.New("TLS RPC control client is not initialized")
	}
	a.switchStateMu.Lock()
	if known := a.switchStateKnown[id]; known && a.switchState[id] == state {
		a.switchStateMu.Unlock()
		return nil
	}
	a.switchStateMu.Unlock()

	if err := a.rpcClient.SetSwitch(id, state); err != nil {
		if isTimeoutError(err) {
			a.logger.WithFields(logrus.Fields{"id": id, "error": err}).Warn(
				"Switch command timed out awaiting RPC response; command may still be applied by device",
			)
		} else {
			a.logger.WithFields(logrus.Fields{"id": id, "error": err}).Error("Failed to send RPC commnand")
		}
		return err
	}
	a.switchStateMu.Lock()
	a.switchState[id] = state
	a.switchStateKnown[id] = true
	a.switchStateMu.Unlock()
	return nil
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if timeoutErr, ok := err.(interface{ Timeout() bool }); ok {
		return timeoutErr.Timeout()
	}
	if netErr, ok := err.(net.Error); ok {
		return netErr.Timeout()
	}
	return false
}

func (a *Actioneer) trackingCenter() (int, int) {
	viewport := a.trackingViewport
	if viewport.Empty() {
		viewport = image.Rect(0, 0, a.frameWidth, a.frameHeight)
	}
	return viewport.Min.X + viewport.Dx()/2, viewport.Min.Y + viewport.Dy()/2
}

// Yes I use these, clean up later
func (a *Actioneer) GetPan() float64 {
	return a.currentPan
}

type PanProvider interface {
	GetPan() float64
}
