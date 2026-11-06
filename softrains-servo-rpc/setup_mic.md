# Headless Go Voice Recognition Setup Guide
This guide provides the complete setup documentation for compiling and running a headless, hardware-driven Go application utilizing **Vosk** for speech-to-text recognition and **PortAudio** for live microphone capture on an Armbian Orange Pi. It includes integrated "Hey Joe!" wake-word detection, silence timeout tracking, and localized debug logging.

---

## 1. System Dependencies Installation
Execute the following commands on your Orange Pi to update package lists and install the development header files required for hardware audio streaming and compilation:

```bash
sudo apt update
sudo apt install -y portaudio19-dev libasound2-dev wget unzip git
```

---

## 2. Download and Extract the Vosk C Library
Because the Go bindings interact with underlying C code, you must obtain the native architecture-matched (`aarch64`) shared library objects:

```bash
# Create and move to a dedicated library directory
mkdir -p /home/bigjim/stt_workspace/vosk-libs
cd /home/bigjim/stt_workspace/vosk-libs

# Download and extract the matching Vosk release binaries
wget https://github.com/alphacep/vosk-api/releases/download/v0.3.45/vosk-linux-aarch64-0.3.45.zip
unzip vosk-linux-aarch64-0.3.45.zip
```
*This step drops `libvosk.so` and `vosk_api.h` directly into your target workspace directory.*

---

## 3. Go Module Initialization & Dependencies
Navigate to your Go application development directory, initialize your tracking module, and grab the structural code dependencies:

```bash
cd /home/bigjim/gotalk
go mod init gotalk

# Fetch Go modules for Vosk API and PortAudio hardware abstraction
go get github.com/alphacep/vosk-api/go
go get github.com/gordonklaus/portaudio

# Force sync and download dependent references
go mod tidy
```

---

## 4. Application Source Code (`main.go`)
Create or edit your `main.go` file with the following complete logic loop:

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	vosk "github.com/alphacep/vosk-api/go"
	"github.com/gordonklaus/portaudio"
)

type VoskResult struct {
	Text string `json:"text"`
}

type VoskPartialResult struct {
	Partial string `json:"partial"`
}

func main() {
	portaudio.Initialize()
	defer portaudio.Terminate()

	textOutputChan := make(chan string)

	// Suppress the native Vosk/Kaldi internal C++ stdout log spam completely
	vosk.SetLogLevel(-1)

	go listenToMicrophoneWithWakeWord(textOutputChan)

	// Receives text ONLY when wake word has been verified and speaker is finished
	for completedText := range textOutputChan {
		fmt.Printf("[SHIPPER] 🚀 Shipping complete utterance payload: %s\n", completedText)
	}
}

func listenToMicrophoneWithWakeWord(outputChan chan<- string) {
	modelPath := "/home/bigjim/stt_workspace/nerd-dictation/model"
	sampleRate := 16000.0

	model, err := vosk.NewModel(modelPath)
	if err != nil {
		log.Fatalf("Vosk model initialization failed: %v", err)
	}
	defer model.Free()

	recognizer, err := vosk.NewRecognizer(model, sampleRate)
	if err != nil {
		log.Fatalf("Vosk recognizer initialization failed: %v", err)
	}
	defer recognizer.Free()

	inputBuffer := make([]int16, 4096)
	stream, err := portaudio.OpenDefaultStream(1, 0, sampleRate, len(inputBuffer), inputBuffer)
	if err != nil {
		log.Fatalf("Failed to open hardware microphone stream: %v", err)
	}
	if err := stream.Start(); err != nil {
		log.Fatalf("Failed to start hardware audio stream: %v", err)
	}
	defer stream.Stop()
	defer stream.Close()

	fmt.Println("[SPEECH] Listening... Say 'Hey Joe' to activate voice transmission.")

	var lastSpeechTime time.Time
	var textAccumulator string
	var lastLoggedPartial string
	
	isAwake := false
	wakeWord := "hey joe"
	silenceThreshold := 1500 * time.Millisecond

	for {
		err := stream.Read()
		if err != nil {
			continue
		}

		byteBuffer := make([]byte, len(inputBuffer)*2)
		for i, val := range inputBuffer {
			byteBuffer[i*2] = byte(val & 0xff)
			byteBuffer[i*2+1] = byte(val >> 8)
		}

		state := recognizer.AcceptWaveform(byteBuffer)

		if state == 1 {
			rawJson := recognizer.Result()
			var res VoskResult
			if err := json.Unmarshal([]byte(rawJson), &res); err == nil && res.Text != "" {
				cleanedText := strings.TrimSpace(strings.ToLower(res.Text))

				if !isAwake {
					if strings.Contains(cleanedText, wakeWord) {
						isAwake = true
						lastSpeechTime = time.Now()
						fmt.Println("[STATE] 🟢 Wake word detected! Session activated.")
						
						if trailingText := strings.SplitN(cleanedText, wakeWord, 2); len(trailingText) > 1 {
							textAccumulator = strings.TrimSpace(trailingText[1])
						}
					}
				} else {
					textAccumulator = strings.TrimSpace(textAccumulator + " " + res.Text)
					lastSpeechTime = time.Now()
				}
			}
			lastLoggedPartial = ""
		} else {
			rawPartialJson := recognizer.PartialResult()
			var part VoskPartialResult
			if err := json.Unmarshal([]byte(rawPartialJson), &part); err == nil && part.Partial != "" {
				lastSpeechTime = time.Now()
				
				if part.Partial != lastLoggedPartial {
					fmt.Printf("[DEBUG] Parsing words: \"%s\"\n", part.Partial)
					lastLoggedPartial = part.Partial
				}
			}
		}

		if isAwake && textAccumulator != "" && time.Since(lastSpeechTime) > silenceThreshold {
			outputChan <- strings.TrimSpace(textAccumulator)
			textAccumulator = "" 
			isAwake = false
			fmt.Println("[STATE] 🔴 Silence limit reached. Session locked. Awaiting 'Hey Joe'...")
		}
	}
}
```

---

## 5. Compiling with CGO
Since the compiler needs visibility into external static header maps and library anchors, run the `go build` execution string with explicit CGO reference injections:

```bash
CGO_CFLAGS="-I/home/bigjim/stt_workspace/vosk-libs" \
CGO_LDFLAGS="-L/home/bigjim/stt_workspace/vosk-libs -lvosk -lportaudio" \
go build -o speech_shipper main.go
```

---

## 6. Runtime Shared Object Linking
To run the compiled binary, the dynamic operating system loader requires explicit knowledge of your `libvosk.so` location.

### Manual One-Time Execution
```bash
LD_LIBRARY_PATH=/home/bigjim/stt_workspace/vosk-libs ./speech_shipper
```

### Permanent Session Configuration
To run the software using standard execution shortcuts without rewriting path chains, store the configurations into your user profile profile:

1. Open user environment profiles:
   ```bash
   nano ~/.bashrc
   ```
2. Inject the path target string at the very bottom line:
   ```bash
   export LD_LIBRARY_PATH=/home/bigjim/stt_workspace/vosk-libs:$LD_LIBRARY_PATH
   ```
3. Reload active profiles into the active SSH shell:
   ```bash
   source ~/.bashrc
   ```
4. Run smoothly:
   ```bash
   ./speech_shipper
   ```