# Softrains Servo & Sensor RPC Server (`softrains-servo-rpc`)

`softrains-servo-rpc` is an embedded hardware RPC server and conversational voice agent designed for Orange Pi 4 Pro and Raspberry Pi nodes. It exposes a TLS/mTLS-secured RPC interface for camera snapshots, servo gimbal actuation, GPIO switch control, and local RAG indexing, while running an onboard speech-to-text (STT) -> remote LLM inference -> text-to-speech (TTS) pipeline for interactive whimsy and home automation skills.

---

## Core Capabilities

- **Sensor & Camera Snapshot Ingestion**: Captures low-latency MJPEG or YUYV frames directly from V4L2 camera devices (`/dev/video0`) and serves them over RPC to `softrains-detector`.
- **Precision Gimbal & GPIO Control**: Drives pan/tilt/roll servos (using thread-locked microsecond spin-wait PWM bit-banging on Orange Pi 4 Pro) and digital GPIO outputs (status LEDs, targeting laser).
- **Voice-Activated Whimsy Pipeline (STT -> Remote LLM -> TTS)**:
  - **Speech-to-Text (`audite.go`)**: Continuously monitors the onboard microphone (`sndi2s4` / `ES8389` or default ALSA capture) using **Vosk** (`vosk-api`) for a configurable wake word (`wakeword` or `"hey you"`).
  - **Remote LLM Inference & Tool Loop (`brains.go`)**: Sends recognized speech to a remote **Ollama** inference server (`OLLAMA_HOST` / `brainApiUrl`) with multi-turn tool calling for:
    - `queryRag`: Searching the local Bleve RAG knowledge base.
    - `readSiteContent`: Fetching live web content.
    - `executeSkill`: Triggering external HTTP `GET` automation skills (lights, motors, switches) with environment-variable token interpolation.
  - **Text-to-Speech (`aawaz.go`)**: Speaks both LLM responses and RPC-triggered alerts (`GimbalRPC.SetSpeech`) through `espeak-ng`, automatically pausing and reopening the microphone capture stream during playback so the shared I2S codec never collides.
- **Local RAG Knowledge Base (`rag.go` & `cmd/ragindexer`)**: Embedded Bleve full-text vector/BM25 store accessible via RPC or the standalone `ragindexer` CLI utility.

---

## Architecture Overview

```text
+-------------------------------------------------------------------------+
|                           softrains-servo-rpc                           |
|                                                                         |
|  [Microphone (ALSA/PortAudio)]                                          |
|        |                                                                |
|        v                                                                |
|  +--------------------+     Wake Word + Prompt    +------------------+  |
|  | Vosk STT Recognizer| ------------------------> | SoftrainsBrains  |  |
|  | (internal/speech)  |                           | (internal/brains)|  |
|  +---------+----------+                           +--------+---------+  |
|            |                                               |            |
|            | Pause/Resume Stream                           | Ollama API |
|            v                                               v            |
|  +--------------------+ <------------------------ [Remote LLM Server]   |
|  | Aawaz TTS Engine   |     Spoken Response                |            |
|  | (espeak-ng)        |                                    +--> Tools:  |
|  +---------+----------+                                    |  - queryRag|
|            ^                                               |  - readSite|
|            |                                               |  - skills  |
|  +---------+-----------------------------------------------+---------+  |
|  |                  GimbalRPC Server (TLS 1.3 / mTLS)                |  |
|  |  - GetSnapshotJPEG (V4L2)   - SetAxis / SetSwitch / GetState      |  |
|  |  - SetSpeech                - SaveData / QueryData (Bleve RAG)    |  |
|  +-------------------------------------------------------------------+  |
+-------------------------------------------------------------------------+
```

---

## `GimbalRPC` API Interface

The RPC server listens on `listenAddress` (default `0.0.0.0:9444`) using Go's `net/rpc` over **TLS 1.3** (with optional **mTLS** client certificate verification via `clientCAFile` and `requireClientCert`).

### 1. `GimbalRPC.Ping`
Health check endpoint.

**Request:**
```go
struct{}{}
```

**Reply (`PingReply`):**
```go
type PingReply struct {
    Ok      bool   // true
    Message string // "pong"
}
```

---

### 2. `GimbalRPC.GetSnapshotJPEG`
Captures a single JPEG frame from the configured V4L2 video device (`/dev/video0`).

**Request (`SnapshotRequest`):**
```go
type SnapshotRequest struct{}
```

**Reply (`SnapshotReply`):**
```go
type SnapshotReply struct {
    Ok          bool
    Message     string
    ContentType string // "image/jpeg"
    JPEG        []byte // Raw JPEG image bytes
}
```

---

### 3. `GimbalRPC.SetAxis`
Moves an enabled gimbal servo axis (`"pan"`, `"tilt"`, or `"roll"`) to the target angle in degrees.

**Request (`SetAxisRequest`):**
```go
type SetAxisRequest struct {
    Axis     string  // "pan", "tilt", or "roll"
    Position float64 // Target angle in degrees (clamped to [min, max])
}
```

**Reply (`GenericReply`):**
```go
type GenericReply struct {
    Ok      bool
    Message string
}
```

---

### 4. `GimbalRPC.SetSwitch`
Toggles a configured digital GPIO output switch (status LEDs, targeting laser) by its numeric `id`.

**Request (`SetSwitchRequest`):**
```go
type SetSwitchRequest struct {
    ID    int  // Switch ID defined in config.json (e.g., 1..4)
    State bool // true = ON, false = OFF
}
```

**Reply (`GenericReply`):**
```go
type GenericReply struct {
    Ok      bool
    Message string
}
```

---

### 5. `GimbalRPC.GetState`
Returns the current position of all gimbal axes and the state of all digital switches.

**Request:**
```go
struct{}{}
```

**Reply (`StateReply`):**
```go
type ControllerState struct {
    Pan      float64      `json:"pan"`
    Tilt     float64      `json:"tilt"`
    Roll     float64      `json:"roll"`
    Laser    bool         `json:"laser"`
    Switches map[int]bool `json:"switches"`
}

type StateReply struct {
    Ok    bool
    State ControllerState
}
```

---

### 6. `GimbalRPC.SetSpeech`
Speaks the provided text through the shared `Aawaz` (`espeak-ng`) audio pathway, automatically pausing and resuming the Vosk microphone listener so voice activation remains uninterrupted.

**Request:**
```go
string // Text to synthesize
```

**Reply (`GenericReply`):**
```go
type GenericReply struct {
    Ok      bool
    Message string
}
```

---

### 7. `GimbalRPC.SaveData`
Indexes a document chunk into the local Bleve RAG store.

**Request (`SaveDataRequest`):**
```go
type SaveDataRequest struct {
    ID      string // Unique document or chunk identifier
    Content string // Text content to index
}
```

**Reply (`GenericReply`):**
```go
type GenericReply struct {
    Ok      bool
    Message string
}
```

---

### 8. `GimbalRPC.QueryData`
Searches the local Bleve RAG store for passages matching `Query`.

**Request (`QueryDataRequest`):**
```go
type QueryDataRequest struct {
    Query string
}
```

**Reply (`QueryDataReply`):**
```go
type QueryDataReply struct {
    Ok               bool
    Message          string
    RetrievedContext []string
    OllamaResponse   string
}
```

---

## Configuration (`config.json`)

```json
{
  "listenAddress": "0.0.0.0:9444",
  "tls": {
    "certFile": "server.crt",
    "keyFile": "server.key",
    "clientCAFile": "",
    "requireClientCert": false
  },
  "board": "orange",
  "pan": {
    "pin": 356,
    "min": 0.0,
    "max": 180.0,
    "home": 90.0,
    "enabled": true
  },
  "pwm": {
    "minPulseWidth": 500,
    "maxPulseWidth": 2500,
    "periodMs": 20,
    "pulseCount": 50,
    "signalDurationMs": 1000
  },
  "switches": [
    { "id": 1, "enabled": true, "pin": 96,  "activeHigh": true, "defaultOn": false, "name": "blue_wire" },
    { "id": 2, "enabled": true, "pin": 97,  "activeHigh": true, "defaultOn": false, "name": "laser" },
    { "id": 3, "enabled": true, "pin": 98,  "activeHigh": true, "defaultOn": false, "name": "green_wire" },
    { "id": 4, "enabled": true, "pin": 100, "activeHigh": true, "defaultOn": false, "name": "red_wire" }
  ],
  "snapshot": {
    "enabled": true,
    "device": "/dev/video0",
    "inputFormat": "mjpeg",
    "width": 1280,
    "height": 720,
    "framerate": 15,
    "quality": 2,
    "timeoutMs": 3000,
    "maxBytes": 4194304
  },
  "wakeword": "Hey Asshole",
  "speechModelPath": "/home/bigjim/stt_workspace/vosk-libs/nerd-dictation/model/vosk-model-small-en-us-0.15",
  "brainApiUrl": "http://192.168.68.103:11434",
  "aiModel": "llama3.1:latest",
  "speakCommand": [
    "sudo", "-u", "bigjim",
    "XDG_RUNTIME_DIR=/run/user/1000",
    "PULSE_SERVER=unix:/run/user/1000/pulse/native",
    "espeak-ng", "-s", "160", "-g", "10"
  ],
  "rag": {
    "indexPath": "/home/bigjim/rag_index"
  },
  "skills": [
    {
      "name": "desk_light_on",
      "description": "Turns on the desk or workspace light when the user asks for light at the desk or office.",
      "url": "http://localhost:8080/api/device/desk_light/on?token={token}",
      "tokenEnv": "SOFTRAINS_SKILL_TOKEN"
    }
  ]
}
```

### Configurable LLM Skills (`skills`)
Each entry in `skills` defines a device or automation action exposed to the LLM via the `executeSkill` tool:
- `name`: Unique identifier for the skill.
- `description`: Natural-language description used by the model to select the appropriate action based on the user's spoken request.
- `url`: Target HTTP `GET` URL. Supports `{token}`, `{{token}}`, `<token>`, `%s`, or `{ENV_VAR_NAME}` placeholders populated from the environment.
- `tokenEnv`: Environment variable name holding the secret token (falls back to `SOFTRAINS_SKILL_TOKEN`, `SKILL_TOKEN`, or `SOFTRAINS_TOKEN`).

---

## Building & Running

### Build Both Binaries (`gimbalrpc-arm64` & `ragindexer-arm64`)
```bash
make build
```

### Run the RPC & Voice Server
```bash
sudo LD_LIBRARY_PATH=/home/bigjim/stt_workspace/vosk-libs bin/gimbalrpc-arm64 -config config.json
```

### Index Documents into Local RAG
```bash
# While gimbalrpc-arm64 is running (via live GimbalRPC.SaveData RPC):
./bin/ragindexer-arm64 -path ./docs

# Or offline directly into the Bleve index directory:
./bin/ragindexer-arm64 -direct -config config.json -path ./docs
```
