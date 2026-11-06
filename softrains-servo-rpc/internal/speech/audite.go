package speech

import (
	"encoding/json"
	"errors"
	"softrains-servo-rpc/internal/brains"
	"softrains-servo-rpc/internal/config"
	"softrains-servo-rpc/internal/gpio"
	"softrains-servo-rpc/internal/rag"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gordonklaus/portaudio"
	vosk "github.com/kercre123/vosk-api/go"
	"github.com/sirupsen/logrus"
)

type SpeechRecognizer struct {
	modelPath     string
	logger        *logrus.Logger
	wakeWord      string
	board         gpio.Board
	brains        *brains.SoftrainsBrains
	talker        *Aawaz
	greenPin      int
	bluePin       int
	redPin        int
	laserPin      int
	activeHigh    bool
	streamMu      sync.Mutex
	stream        *portaudio.Stream
	paused        atomic.Bool
	resetState    atomic.Bool
	deviceLogged  bool
}

// VoskResult structures represent the distinct JSON variations output by the engine
type VoskResult struct {
	Text string `json:"text"`
}

type VoskPartialResult struct {
	Partial string `json:"partial"`
}

func NewSpeechRecognizer(board gpio.Board, cfg *config.Config, logger *logrus.Logger, ragEngine *rag.Engine) (*SpeechRecognizer, error) {
	brains, err := brains.NewSoftrainsBrains(cfg, logger, ragEngine)
	if err != nil {
		return nil, err
	}

	greenPin := 98
	bluePin := 97
	redPin := 100
	laserPin := cfg.Laser.Pin
	activeHigh := true

	for _, sw := range cfg.Switches {
		if !sw.Enabled {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(sw.Name)) {
		case "green_wire", "green":
			greenPin = sw.Pin
			activeHigh = sw.ActiveHigh
		case "blue_wire", "blue":
			bluePin = sw.Pin
			activeHigh = sw.ActiveHigh
		case "red_wire", "red":
			redPin = sw.Pin
			activeHigh = sw.ActiveHigh
		case "laser":
			laserPin = sw.Pin
		}
	}

	talker := NewAawaz(logger, cfg.SpeakCommand)
	sr := &SpeechRecognizer{
		modelPath:  cfg.SpeechModelPath,
		logger:     logger,
		wakeWord:   cfg.WakeWord,
		board:      board,
		brains:     brains,
		talker:     talker,
		greenPin:   greenPin,
		bluePin:    bluePin,
		redPin:     redPin,
		laserPin:   laserPin,
		activeHigh: activeHigh,
	}

	talker.SetHooks(sr.pauseMicForSpeech, sr.resumeMicAfterSpeech)
	return sr, nil
}

// Talker returns the shared Aawaz speech engine that coordinates with the microphone stream.
func (sr *SpeechRecognizer) Talker() *Aawaz {
	if sr == nil {
		return nil
	}
	return sr.talker
}

func (sr *SpeechRecognizer) pauseMicForSpeech() {
	sr.paused.Store(true)
	sr.streamMu.Lock()
	defer sr.streamMu.Unlock()
	if sr.stream != nil {
		_ = sr.stream.Stop()
		_ = sr.stream.Close()
		sr.stream = nil
		sr.logger.Debug("Paused microphone stream while speaking")
	}
}

func (sr *SpeechRecognizer) resumeMicAfterSpeech() {
	sr.resetState.Store(true)
	sr.paused.Store(false)
}

// StartSpeechRecognition initializes the microphone stream and continuously listens for speech input.
// When a wake word is detected, it begins listening
// When a sentence is finished 1.5 second break in speech, it sends the complete text to the output channel for further processing.

func (sr *SpeechRecognizer) StartSpeechRecognition() error {
	portaudio.Initialize()
	defer portaudio.Terminate()

	textOutputChan := make(chan string)

	// Suppress the native Vosk/Kaldi internal C++ stdout log spam completely
	vosk.SetLogLevel(-1)

	go func() {
		if err := sr.listenToMicrophoneWithWakeWord(textOutputChan); err != nil {
			sr.logger.Errorf("microphone listener stopped with error: %v", err)
		}
	}()

	// Receives text ONLY when wake word has been verified and speaker is finished
	for completedText := range textOutputChan {
		// send to softrains-brains to get a response
		sr.logger.Debugf("[SHIPPER] 🚀 Shipping complete sentence payload: %s\n", completedText)
		sr.board.SetDigital(sr.greenPin, false, sr.activeHigh) // green off
		sr.board.SetDigital(sr.bluePin, true, sr.activeHigh)   // blue on

		resp, err := sr.sendTextToBrains(completedText)
		if err != nil {
			sr.logger.Errorf("failed to get response from softrains-brains: %v", err)
			sr.board.SetDigital(sr.greenPin, false, sr.activeHigh)
			sr.board.SetDigital(sr.bluePin, false, sr.activeHigh)
			continue
		}
		// call aawaz to speak the response from softrains-brains
		sr.board.SetDigital(sr.greenPin, true, sr.activeHigh) // green on
		if err := sr.talker.Speak(resp); err != nil {
			sr.logger.Errorf("failed to speak response: %v", err)
		}
		sr.board.SetDigital(sr.greenPin, false, sr.activeHigh) // everything off
		sr.board.SetDigital(sr.bluePin, false, sr.activeHigh)
	}
	return nil
}

func (sr *SpeechRecognizer) openMicStream(sampleRate float64, inputBuffer []int16) (*portaudio.Stream, error) {
	var stream *portaudio.Stream
	var err error
	devices, devErr := portaudio.Devices()
	if devErr == nil {
		var selectedDev *portaudio.DeviceInfo
		for _, dev := range devices {
			if dev.MaxInputChannels <= 0 {
				continue
			}
			if !sr.deviceLogged {
				sr.logger.Infof("Found audio input device: %q (maxInputChannels=%d, defaultSampleRate=%.0f)", dev.Name, dev.MaxInputChannels, dev.DefaultSampleRate)
			}
			nameLower := strings.ToLower(dev.Name)
			if strings.Contains(nameLower, "hdmi") {
				continue
			}
			if strings.Contains(nameLower, "sndi2s4") || strings.Contains(nameLower, "es8389") || strings.Contains(nameLower, "default") || strings.Contains(nameLower, "pulse") {
				selectedDev = dev
				if strings.Contains(nameLower, "sndi2s4") || strings.Contains(nameLower, "es8389") {
					break
				}
			} else if selectedDev == nil {
				selectedDev = dev
			}
		}
		sr.deviceLogged = true
		if selectedDev != nil {
			sr.logger.Debugf("Opening audio input device: %q", selectedDev.Name)
			params := portaudio.StreamParameters{
				Input: portaudio.StreamDeviceParameters{
					Device:   selectedDev,
					Channels: 1,
					Latency:  selectedDev.DefaultHighInputLatency,
				},
				SampleRate:      sampleRate,
				FramesPerBuffer: len(inputBuffer),
			}
			stream, err = portaudio.OpenStream(params, inputBuffer)
			if err != nil {
				sr.logger.Warnf("OpenStream on %q at %.0fHz failed (%v), falling back to OpenDefaultStream", selectedDev.Name, sampleRate, err)
				stream = nil
			}
		}
	}

	if stream == nil {
		stream, err = portaudio.OpenDefaultStream(1, 0, sampleRate, len(inputBuffer), inputBuffer)
		if err != nil {
			return nil, err
		}
	}
	if err := stream.Start(); err != nil {
		_ = stream.Close()
		return nil, err
	}
	return stream, nil
}

func (sr *SpeechRecognizer) listenToMicrophoneWithWakeWord(outputChan chan<- string) error {
	sampleRate := 16000.0
	sr.logger.Infof("Loading Vosk speech model from %s...", sr.modelPath)
	model, err := vosk.NewModel(sr.modelPath)
	if err != nil {
		return err
	}
	defer model.Free()

	recognizer, err := vosk.NewRecognizer(model, sampleRate)
	if err != nil {
		return err
	}
	defer recognizer.Free()

	inputBuffer := make([]int16, 4096)

	defer func() {
		sr.streamMu.Lock()
		if sr.stream != nil {
			_ = sr.stream.Stop()
			_ = sr.stream.Close()
			sr.stream = nil
		}
		sr.streamMu.Unlock()
	}()

	wakeWord := strings.ToLower(strings.TrimSpace(sr.wakeWord))
	if wakeWord == "" {
		wakeWord = "hey you"
	}
	sr.logger.Infof("Listening for wake word (%q or \"hey you\") and speech input...", wakeWord)

	// Local State Configuration
	var lastSpeechTime time.Time
	var textAccumulator string
	var lastLoggedPartial string

	isAwake := false
	silenceThreshold := 1500 * time.Millisecond

	// Optimization: Allocate byte buffer once outside the hot loop to reduce GC pressure
	byteBuffer := make([]byte, len(inputBuffer)*2)

	for {
		if sr.paused.Load() {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		sr.streamMu.Lock()
		if sr.paused.Load() {
			sr.streamMu.Unlock()
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if sr.stream == nil {
			s, openErr := sr.openMicStream(sampleRate, inputBuffer)
			if openErr != nil {
				sr.streamMu.Unlock()
				sr.logger.Warnf("Waiting to reopen microphone stream: %v", openErr)
				time.Sleep(250 * time.Millisecond)
				continue
			}
			sr.stream = s
			sr.logger.Debug("Microphone capture stream active")
		}
		err := sr.stream.Read()
		if err != nil && !errors.Is(err, portaudio.InputOverflowed) {
			_ = sr.stream.Stop()
			_ = sr.stream.Close()
			sr.stream = nil
			sr.streamMu.Unlock()
			sr.logger.Debugf("Microphone stream read interrupted (%v), reopening stream...", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		sr.streamMu.Unlock()

		if sr.resetState.CompareAndSwap(true, false) {
			recognizer.Reset()
			lastLoggedPartial = ""
			if !isAwake {
				textAccumulator = ""
			}
		}

		// Map hardware buffer data explicitly to raw byte slice
		for i, val := range inputBuffer {
			byteBuffer[i*2] = byte(val & 0xff)
			byteBuffer[i*2+1] = byte(val >> 8)
		}

		state := recognizer.AcceptWaveform(byteBuffer)

		if state == 1 {
			rawJson := recognizer.Result()
			var res VoskResult

			// FIX 1: Only catch structural unmarshal errors, don't exit loop on empty text
			if err := json.Unmarshal([]byte(rawJson), &res); err != nil {
				sr.logger.Errorf("JSON unmarshal error on structural break: %v", err)
				continue
			}

			if res.Text != "" {
				cleanedText := strings.TrimSpace(strings.ToLower(res.Text))

				if !isAwake {
					matchedWakeWord := ""
					if strings.Contains(cleanedText, wakeWord) {
						matchedWakeWord = wakeWord
					} else if strings.Contains(cleanedText, "hey you") {
						matchedWakeWord = "hey you"
					}
					if matchedWakeWord != "" {
						isAwake = true
						lastSpeechTime = time.Now()
						sr.logger.Debugf("[STATE] 🟢 Wake word detected! Session activated.")
						sr.board.SetDigital(sr.greenPin, true, sr.activeHigh) // green on
						sr.board.SetDigital(sr.bluePin, false, sr.activeHigh) // blue off
						if trailingText := strings.SplitN(cleanedText, matchedWakeWord, 2); len(trailingText) > 1 {
							textAccumulator = strings.TrimSpace(trailingText[1])
						}
					}
				} else {
					textAccumulator = strings.TrimSpace(textAccumulator + " " + res.Text)
					// Only update the silence clock when the user explicitly speaks valid text
					lastSpeechTime = time.Now()
				}
			}
			lastLoggedPartial = ""
		} else {
			rawPartialJson := recognizer.PartialResult()
			var part VoskPartialResult
			if err := json.Unmarshal([]byte(rawPartialJson), &part); err == nil && part.Partial != "" {
				// FIX 2: Only reset the silence watchdog if the robot is actually awake
				// This stops environmental background noise from keeping the loop awake forever
				if isAwake {
					lastSpeechTime = time.Now()
				}

				if part.Partial != lastLoggedPartial {
					sr.logger.Debugf("[DEBUG] Parsing words: \"%s\"\n", part.Partial)
					lastLoggedPartial = part.Partial
				}
			}
		}

		// Watchdog execution loop
		if isAwake && textAccumulator != "" && time.Since(lastSpeechTime) > silenceThreshold {
			outputChan <- strings.TrimSpace(textAccumulator)
			// Flush buffers and cleanly reset state tracking
			textAccumulator = ""
			lastLoggedPartial = ""
			isAwake = false
			sr.logger.Debug("[STATE] 🔴 Silence limit reached. Session locked. Awaiting wake word...")
		}
	}
}

func (sr *SpeechRecognizer) sendTextToBrains(text string) (string, error) {
	sr.logger.Debugf("[SHIPPER] Sending text to softrains-brains: %s\n", text)
	// Setup http rest call to AI chat interface to get a response and then send that response to the servo controller to move the gimbal
	resp, err := sr.brains.CallOllama(text)
	if err != nil {
		return "", err
	}

	sr.logger.Debugf("[SHIPPER] Received response from softrains-brains: %s\n", resp)
	return resp, nil
}
