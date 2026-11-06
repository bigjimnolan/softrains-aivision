package startup

import (
	"encoding/json"
	"fmt"
	"os"
)

type CameraType string

const (
	CameraTypeStandard          CameraType = "standard"
	CameraTypeStereoLeft        CameraType = "stereo-left"
	CameraTypeZerocam           CameraType = "zerocam"
	CameraTypeFisheyeStereoLeft CameraType = "fisheye-stereo-left"
	CameraTypeStereo            CameraType = "stereo-left"
)

// ActionTrigger configures how Actioneer chooses a target/action in order.
// Triggers are evaluated in the order provided by config.
type ActionTriggerType string

const (
	ActionTriggerDefaultTarget ActionTriggerType = "default-target"
	ActionTriggerLeftWrist     ActionTriggerType = "left-wrist"
	ActionTriggerToggleLaser   ActionTriggerType = "toggle-laser"
)

type ActionTrigger struct {
	Type          ActionTriggerType `json:"type"`
	MinConfidence float32           `json:"minConfidence,omitempty"`
	TimeoutMs     int               `json:"timeoutMs,omitempty"`
}

type AxisConfig struct {
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Center float64 `json:"center"`
}

type BotControlConfig struct {
	URL         string     `json:"url"`
	Protocol    string     `json:"protocol"`
	RPCEndpoint string     `json:"rpcEndpoint,omitempty"`
	ServerName  string     `json:"serverName,omitempty"`
	CACert      string     `json:"caCert,omitempty"`
	Pan         AxisConfig `json:"pan"`
	Tilt        AxisConfig `json:"tilt"`
	Roll        AxisConfig `json:"roll"`
}

type CameraConfig struct {
	Index       int                `json:"index"`
	Width       int                `json:"width"`
	Height      int                `json:"height"`
	FOV         float64            `json:"fov"`
	URL         string             `json:"url"`
	Quality     int                `json:"quality"` // 0-6 maps to ZeroCam ql= command; 0 means use camera default
	Orientation *OrientationConfig `json:"orientation"`
}

type OrientationConfig struct {
	Rotate         int  `json:"rotate"`
	FlipHorizontal bool `json:"flipHorizontal"`
	FlipVertical   bool `json:"flipVertical"`
}

type ConnectionConfig struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Source      string           `json:"source"`
	Camera      *CameraConfig    `json:"camera,omitempty"`
	CameraType  string           `json:"cameraType"`
	Control     BotControlConfig `json:"control"`
}

type DetectionFiltersConfig struct {
	MinBoxWidth    int     `json:"minBoxWidth"`
	MinBoxHeight   int     `json:"minBoxHeight"`
	MinBoxArea     int     `json:"minBoxArea"`
	MaxAreaRatio   float32 `json:"maxAreaRatio"`
	MaxWidthRatio  float32 `json:"maxWidthRatio"`
	MaxHeightRatio float32 `json:"maxHeightRatio"`
	UsePose        bool    `json:"usePose"`
}

type AIConfig struct {
	Name                string                 `json:"name"`
	RPCEndpoint         string                 `json:"rpcEndpoint"`
	ServerName          string                 `json:"serverName"`
	CACert              string                 `json:"caCert"`
	InputWidth          int                    `json:"inputWidth"`
	InputHeight         int                    `json:"inputHeight"`
	ConfidenceThreshold float32                `json:"confidenceThreshold"`
	NMSThreshold        float32                `json:"nmsThreshold"`
	ProcessEveryNFrames int                    `json:"processEveryNFrames"`
	ColorAnalysis       ColorAnalysisConfig    `json:"colorAnalysis"`
	DetectionFilters    DetectionFiltersConfig `json:"detectionFilters"`
}

type ColorAnalysisConfig struct {
	Enabled    bool   `json:"enabled"`
	Mode       string `json:"mode"`
	SampleStep int    `json:"sampleStep"`
}

type MarkingModeConfig struct {
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
}

type LaserConfig struct {
	Enabled bool `json:"enabled"`
}

type ScanModeConfig struct {
	Enabled        bool `json:"enabled"`
	ScanRange      int  `json:"scanRange"`
	ScanStep       int  `json:"scanStep"`
	ScanIntervalMs int  `json:"scanIntervalMs"`
}

type TrackingConfig struct {
	PanSensitivity         float64           `json:"panSensitivity"`
	TiltSensitivity        float64           `json:"tiltSensitivity"`
	DeadZone               int               `json:"deadZone"`
	ControlTimeoutMs       int               `json:"controlTimeoutMs"`
	ControlRetryIntervalMs int               `json:"controlRetryIntervalMs,omitempty"`
	DetectionTimeoutMs     int               `json:"detectionTimeoutMs,omitempty"`
	ActionTriggers         []ActionTrigger   `json:"actionTriggers"`
	MarkingMode            MarkingModeConfig `json:"markingMode"`
	Laser                  LaserConfig       `json:"laser"`
	ScanMode               ScanModeConfig    `json:"scanMode"`
}

type LoggingConfig struct {
	Level  string `json:"level"`
	Format string `json:"format"`
}

type DetectorConfig struct {
	Enabled           *bool  `json:"enabled"`
	WindowTitle       string `json:"windowTitle"`
	FrameInfoInterval int    `json:"frameInfoInterval"`
	RotateDisplay     int    `json:"rotateDisplay"`
	Headless          *bool  `json:"headless"`
}

type AppConfig struct {
	AI              []AIConfig         `json:"ai"`
	Logging         LoggingConfig      `json:"logging"`
	Connections     []ConnectionConfig `json:"connections"`
	StartupProfiles []StartupProfile   `json:"startupProfiles"`
}

type StartupProfile struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	BotSetup    SoftrainsVisionBotProfile `json:"botSetup"`
	Detector    DetectorConfig            `json:"display"`
}

type SoftrainsVisionBotProfile struct {
	ConnectionName string           `json:"connectionName"`
	AIName         string           `json:"aiName"`
	Connection     ConnectionConfig `json:"connection"`
	Tracking       TrackingConfig   `json:"tracking"`
	AI             AIConfig         `json:"ai"`
}

type PoseKeyMap struct {
	PoseKeyMap map[string]string `json:"pose_key_map"`
}

func LoadAppConfig(configPath string) (*AppConfig, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", configPath, err)
	}
	defer file.Close()

	var cfg AppConfig
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode %s: %w", configPath, err)
	}
	return &cfg, nil
}

func LoadDetectorLabels(configPath string) (*PoseKeyMap, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", configPath, err)
	}
	defer file.Close()

	var cfg PoseKeyMap
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode %s: %w", configPath, err)
	}
	return &cfg, nil
}
