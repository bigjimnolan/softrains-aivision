# Softrains AI Vision (`softrains-aivision`)

`softrains-aivision` is a fully self-hosted, local-first AI computer vision, robotics control, and conversational voice-automation platform inspired by Ray Bradbury's *"There Will Come Soft Rains"*.

The entire stack is designed to run **100% locally on your own hardware** with no cloud dependencies:
- **Edge Sensor & Actuator Node**: Runs on an **Orange Pi 4 Pro** (`Allwinner A733` ARM64), handling V4L2 camera capture, PWM servo gimbal control, GPIO indicator/laser switching, local Bleve RAG storage, offline wake-word speech recognition (**Vosk**), and local speech synthesis (**`espeak-ng`**).
- **Local LLM & Vision Inference**: Conversational intelligence and tool-calling (`llama3.1` via **Ollama**) run on a local host powered by an **NVIDIA GTX/RTX 3060 GPU**, alongside local AI vision/pose detection—keeping all video frames, voice audio, and home-automation triggers strictly on the local network.

---

## Repository Structure

### 1. [`softrains-servo-rpc/`](./softrains-servo-rpc/README.md) — Edge Sensor, Gimbal & Voice RPC Node
Runs directly on the **Orange Pi 4 Pro** and acts as the physical hardware interface and voice gateway:
- **`GimbalRPC` Server (TLS 1.3 / mTLS)**: Exposes RPC endpoints for fetching V4L2 camera snapshots (`GetSnapshotJPEG`), positioning pan/tilt/roll servos (`SetAxis`), toggling GPIO switches and lasers (`SetSwitch`), querying board state (`GetState`), triggering spoken alerts (`SetSpeech`), and indexing/searching local documents (`SaveData`, `QueryData`).
- **Voice-Activated Whimsy & Automation (`internal/speech` & `internal/brains`)**:
  - Listens continuously on the onboard microphone via **Vosk** for the wake word.
  - Streams transcribed prompts to the local **Ollama** LLM running on the **GTX/RTX 3060** host.
  - Executes multi-turn LLM tool calls against the local **Bleve RAG** index (`queryRag`), live web endpoints (`readSiteContent`), and configurable HTTP `GET` device skills (`executeSkill` with environment-token interpolation for lights, motors, and switches).
  - Speaks responses back through `espeak-ng` while automatically coordinating microphone capture access on the shared I2S audio codec.
- **`ragindexer` CLI (`cmd/ragindexer`)**: Utility for chunking and indexing local Markdown/text documents into the node's Bleve RAG store.

### 2. [`softrains-detector/`](./softrains-detector/README.md) — AI Vision Processing & Closed-Loop Tracker
Polls camera telemetry from `softrains-servo-rpc`, runs human and pose detection, and drives real-time physical tracking:
- **Snapshot Polling & Preprocessing (`pkg/imageprocessing`)**: Fetches JPEG frames from the Orange Pi 4 Pro over TLS RPC (`GimbalRPC.GetSnapshotJPEG`), applies camera orientation corrections, and optionally renders a live annotated OpenCV (`gocv`) debug viewer.
- **Vision & Pose Detection (`pkg/softrainsvision` & `pkg/softrainsrpc`)**: Evaluates frames against the local AI inference service (`softrains-rpc-v1`) to detect humans and skeletal pose keypoints.
- **Closed-Loop `Actioneer` (`pkg/softrainsvision/actioneer.go`)**:
  - Sweeps the room in autonomous scan mode when idle.
  - Locks onto detected humans using FOV-aware angular pan/tilt adjustments, switches status LEDs, and triggers spoken alerts (`GimbalRPC.SetSpeech`) with a configurable 2-minute cooldown timer (`detectionTimeoutMs`) that resets on each subsequent detection.
- **Hardened Transport**: Supports standard TLS 1.3 server verification and drop-in **Mutual TLS (mTLS)** client certificate authentication without adding application-layer complexity.

---

## Quick Start

1. **Start the Edge RPC & Voice Server (on the Orange Pi 4 Pro)**:
   ```bash
   cd softrains-servo-rpc
   make build
   sudo LD_LIBRARY_PATH=/home/bigjim/stt_workspace/vosk-libs bin/gimbalrpc-arm64 -config config.json
   ```
2. **Start the AI Vision Detector & Tracker**:
   ```bash
   cd softrains-detector
   make build
   ./bin/softrains-detector
   ```

See the detailed documentation in [`softrains-servo-rpc/README.md`](./softrains-servo-rpc/README.md) and [`softrains-detector/README.md`](./softrains-detector/README.md) for full RPC schemas and configuration options.
