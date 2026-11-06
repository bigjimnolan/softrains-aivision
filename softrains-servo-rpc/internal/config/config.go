package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type AxisConfig struct {
	Pin     int     `json:"pin"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	Home    float64 `json:"home"`
	Enabled bool    `json:"enabled"`
}

type PWMConfig struct {
	MinPulseWidth    int    `json:"minPulseWidth"`
	MaxPulseWidth    int    `json:"maxPulseWidth"`
	PeriodMs         int    `json:"periodMs"`
	PulseCount       int    `json:"pulseCount"`
	SignalDurationMs int    `json:"signalDurationMs"`
	Comment          string `json:"comment"`
}

type TLSConfig struct {
	CertFile          string `json:"certFile"`
	KeyFile           string `json:"keyFile"`
	ClientCAFile      string `json:"clientCAFile"`
	RequireClientCert bool   `json:"requireClientCert"`
}

type DigitalOutputConfig struct {
	Enabled    bool   `json:"enabled"`
	Pin        int    `json:"pin"`
	ActiveHigh bool   `json:"activeHigh"`
	DefaultOn  bool   `json:"defaultOn"`
	Name       string `json:"name"`
}

type SwitchConfig struct {
	ID int `json:"id"`
	DigitalOutputConfig
}

type ResourceConfig struct {
	MinAxisCommandIntervalMs int     `json:"minAxisCommandIntervalMs"`
	MinAxisDeltaDegrees      float64 `json:"minAxisDeltaDegrees"`
	MaxConcurrentAxisMoves   int     `json:"maxConcurrentAxisMoves"`
}

type SnapshotConfig struct {
	Enabled     bool   `json:"enabled"`
	Transport   string `json:"transport"`
	Device      string `json:"device"`
	RTSPURL     string `json:"rtspUrl"`
	InputFormat string `json:"inputFormat"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Framerate   int    `json:"framerate"`
	Quality     int    `json:"quality"`
	FFmpegPath  string `json:"ffmpegPath"`
	TimeoutMs   int    `json:"timeoutMs"`
	MaxBytes    int64  `json:"maxBytes"`
}

type RagConfig struct {
	IndexPath string `json:"indexPath"`
}

type SkillConfig struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	TokenEnv    string `json:"tokenEnv,omitempty"`
}

type Config struct {
	ListenAddress   string              `json:"listenAddress"`
	TLS             TLSConfig           `json:"tls"`
	Pan             AxisConfig          `json:"pan"`
	Tilt            AxisConfig          `json:"tilt"`
	Roll            AxisConfig          `json:"roll"`
	PWM             PWMConfig           `json:"pwm"`
	Laser           DigitalOutputConfig `json:"laser"`
	Switches        []SwitchConfig      `json:"switches"`
	Resource        ResourceConfig      `json:"resource"`
	Snapshot        SnapshotConfig      `json:"snapshot"`
	Board           string              `json:"board"`
	WakeWord        string              `json:"wakeWord"`
	SpeechModelPath string              `json:"speechModelPath"`
	BrainAPIURL     string              `json:"brainApiUrl"`
	SpeakCommand    []string            `json:"speakCommand"`
	// Add reference setting
	AiModel string        `json:"aiModel"`
	RAG     RagConfig     `json:"rag"`
	Skills  []SkillConfig `json:"skills,omitempty"`
}

func Load(path string) (*Config, error) {
	var cfg Config

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.ListenAddress == "" {
		return fmt.Errorf("listenAddress is required")
	}
	if c.TLS.CertFile == "" || c.TLS.KeyFile == "" {
		return fmt.Errorf("tls certFile and keyFile are required")
	}
	if c.PWM.MinPulseWidth <= 0 || c.PWM.MaxPulseWidth <= c.PWM.MinPulseWidth {
		return fmt.Errorf("invalid pwm pulse width bounds")
	}
	if c.PWM.PeriodMs <= 0 {
		return fmt.Errorf("pwm periodMs must be > 0")
	}
	if c.PWM.PulseCount <= 0 {
		return fmt.Errorf("pwm pulseCount must be > 0")
	}
	if err := validateAxis("pan", c.Pan); err != nil {
		return err
	}
	if err := validateAxis("tilt", c.Tilt); err != nil {
		return err
	}
	if err := validateAxis("roll", c.Roll); err != nil {
		return err
	}
	if c.Resource.MinAxisCommandIntervalMs < 0 {
		return fmt.Errorf("resource.minAxisCommandIntervalMs cannot be negative")
	}
	if c.Resource.MaxConcurrentAxisMoves <= 0 {
		c.Resource.MaxConcurrentAxisMoves = 1
	}
	if c.Snapshot.FFmpegPath == "" {
		c.Snapshot.FFmpegPath = "ffmpeg"
	}
	if c.Snapshot.Transport == "" {
		c.Snapshot.Transport = "v4l2"
	}
	if c.Snapshot.InputFormat == "" {
		c.Snapshot.InputFormat = "mjpeg"
	}
	if c.Snapshot.Framerate <= 0 {
		c.Snapshot.Framerate = 15
	}
	if c.Snapshot.Quality <= 0 {
		c.Snapshot.Quality = 2
	}
	if c.Snapshot.Quality < 2 {
		c.Snapshot.Quality = 2
	}
	if c.Snapshot.Quality > 31 {
		c.Snapshot.Quality = 31
	}
	if c.Snapshot.TimeoutMs <= 0 {
		c.Snapshot.TimeoutMs = 3000
	}
	if c.Snapshot.MaxBytes <= 0 {
		c.Snapshot.MaxBytes = 4 * 1024 * 1024
	}
	if c.Snapshot.Enabled {
		switch c.Snapshot.Transport {
		case "rtsp":
			if c.Snapshot.RTSPURL == "" {
				return fmt.Errorf("snapshot.rtspUrl is required when snapshot.transport is rtsp")
			}
		default:
			if c.Snapshot.Device == "" {
				return fmt.Errorf("snapshot.device is required when snapshot.transport is v4l2")
			}
		}
	}
	return nil
}

func validateAxis(name string, axis AxisConfig) error {
	if !axis.Enabled {
		return nil
	}
	if axis.Pin <= 0 {
		return fmt.Errorf("%s pin must be > 0", name)
	}
	if axis.Max <= axis.Min {
		return fmt.Errorf("%s max must be greater than min", name)
	}
	if axis.Home < axis.Min || axis.Home > axis.Max {
		return fmt.Errorf("%s home must be within [min, max]", name)
	}
	return nil
}
