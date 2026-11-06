# Softrains Detector (`softrains-detector`)

`softrains-detector` is the AI computer-vision processing and closed-loop tracking service for the Softrains ecosystem. It polls camera snapshots and state telemetry from a remote **`softrains-servo-rpc`** node, runs real-time human and pose detection against a remote AI inference endpoint, and translates visual detections directly into physical hardware actions (pan/tilt servo tracking, status LED/laser switching, and spoken alerts).

---

## Architecture & Data Flow

```text
+------------------------+         TLS / mTLS RPC          +-------------------------+
|  softrains-servo-rpc   | <-----------------------------> |   softrains-detector    |
|  (Sensor & Servo Node) |   1. GimbalRPC.GetSnapshotJPEG  |   (Vision & Actioneer)  |
|                        |   2. GimbalRPC.SetAxis          |                         |
|  - V4L2 USB Camera     |   3. GimbalRPC.SetSwitch        |  - Orientation / Crop   |
|  - Gimbal PWM Servos   |   4. GimbalRPC.SetSpeech        |  - FOV Delta Tracking   |
|  - GPIO LEDs & Laser   |   5. GimbalRPC.GetState         |  - Alert Cooldown Timer |
+------------------------+                                 +------------+------------+
                                                                        |
                                                             TLS / mTLS | (softrains-rpc-v1)
                                                                        v
                                                           +-------------------------+
                                                           |  AI Inference Endpoint  |
                                                           |  (Object + Pose TPU)    |
                                                           +-------------------------+
```

1. **Sensor Ingestion (`DetectionOnlyService`)**:
   - Polls JPEG frames from the `softrains-servo-rpc` node via `GimbalRPC.GetSnapshotJPEG`.
   - Applies configured camera orientation transforms (`rotate`, `flipHorizontal`, `flipVertical`) and viewport cropping (`standard`, `stereo-left`, `fisheye-stereo-left`) before AI processing.
2. **AI Vision Inference (`RPCDetector` / `PoseClient`)**:
   - Scales frames to `300x300` RGB24 and transmits them over TLS to the analytics inference server using the `softrains-rpc-v1` envelope protocol.
   - Filters human detections by confidence, bounding-box geometry, and optional pose keypoints (such as `left_wrist` targeting) and dominant color analysis.
3. **Vision-Driven Direct Input (`Actioneer`)**:
   - Converts pixel offsets from the viewport center into angular pan/tilt adjustments using the camera's configured horizontal field of view (`fov`) and `deadZone`.
   - Drives the remote node's servos (`GimbalRPC.SetAxis`), status LEDs/laser (`GimbalRPC.SetSwitch`), and voice synthesis (`GimbalRPC.SetSpeech`).
   - Maintains a configurable detection cooldown (`detectionTimeoutMs`, default `120000` ms / 2 minutes) that resets on every subsequent detection so scan passes do not repeatedly re-trigger the alert cycle.
   - Automatically resumes sweeping scan mode (`nextScan`) when no targets are observed within `scanIntervalMs`.

---

## RPC Protocols

`softrains-detector` communicates over two lightweight RPC channels layered directly over TLS sockets:

### 1. Node Control & Snapshot RPC (`GimbalRPC`)
Uses Go's standard `net/rpc` over TLS (`pkg/softrainsvision/control_rpc.go` and `pkg/imageprocessing/detection.go`) to interact with `softrains-servo-rpc`:

| RPC Method | Request Type | Reply Type | Purpose |
| :--- | :--- | :--- | :--- |
| `GimbalRPC.GetSnapshotJPEG` | `struct{}` | `SnapshotReply` | Fetches a single V4L2 JPEG frame from the node camera |
| `GimbalRPC.SetAxis` | `SetAxisRequest{Axis, Position}` | `GenericReply` | Commands `"pan"`, `"tilt"`, or `"roll"` servo angles |
| `GimbalRPC.SetSwitch` | `SetSwitchRequest{ID, State}` | `GenericReply` | Toggles onboard GPIO switches (status LEDs, laser) |
| `GimbalRPC.GetState` | `struct{}` | `StateReply` | Reads current servo angles and switch states |
| `GimbalRPC.SetSpeech` | `string` | `GenericReply` | Triggers TTS audio output on the remote node |

### 2. AI Vision Inference RPC (`softrains-rpc-v1`)
Uses a newline-delimited JSON header followed by raw binary RGB24 bytes over TLS (`pkg/softrainsrpc/client.go`):

**Request Header (`softrains-rpc-v1`):**
```json
{
  "schema": "softrains-rpc-v1",
  "service": "pose",
  "command": "detect",
  "request_id": "pose-1730920000",
  "payload_size": 270000,
  "params": {
    "image_format": "rgb24",
    "width": 300,
    "height": 300,
    "pose": true
  }
}
```

**Response Envelope:**
```json
{
  "schema": "softrains-rpc-v1",
  "service": "pose",
  "command": "detect",
  "request_id": "pose-1730920000",
  "success": true,
  "result": {
    "detections": [
      {
        "class_id": 0,
        "confidence": 0.94,
        "x": 0.21,
        "y": 0.14,
        "width": 0.42,
        "height": 0.78,
        "pose": {
          "nose": { "x": 0.51, "y": 0.12, "confidence": 0.91 },
          "left_wrist": { "x": 0.68, "y": 0.49, "confidence": 0.84 }
        }
      }
    ],
    "count": 1,
    "pose_enabled": true
  }
}
```

---

## Hardening Communication with TLS & mTLS

All RPC connections in `softrains-detector` enforce **TLS 1.2 / TLS 1.3** with strict certificate authority (`caCert`) and SAN hostname (`serverName`) verification. Because authentication and encryption happen in the standard `crypto/tls` handshake, you can harden node-to-detector communication with **Mutual TLS (mTLS)** without adding token headers, custom auth handshakes, or application-layer complexity.

### Standard TLS Verification (`config/config.json`)
By default, `softrains-detector` validates the server certificate against a pinned CA certificate and expected `serverName`:

```json
"control": {
  "url": "https://192.168.68.67:9444",
  "rpcEndpoint": "192.168.68.67:9444",
  "serverName": "softrainsvisions-servos-rpc3.local",
  "caCert": "certs/softrainsvision-servos-rpc2.local.crt",
  "protocol": "query-param",
  "pan": {
    "min": 20,
    "max": 120,
    "center": 90
  }
}
```

### Enabling Mutual TLS (mTLS)
1. **On `softrains-servo-rpc` (`config.json`)**:
   Provide the CA certificate that signed the detector's client certificate and set `requireClientCert` to `true`:
   ```json
   "tls": {
     "certFile": "server.crt",
     "keyFile": "server.key",
     "clientCAFile": "certs/root-ca.crt",
     "requireClientCert": true
   }
   ```
2. **In `softrains-detector` (`pkg/softrainsrpc/client.go`)**:
   `softrainsrpc` includes built-in mTLS constructors (`NewTLSConfigWithClientCert` and `NewClientFromCAMTLS`) that attach the client X.509 keypair (`tls.LoadX509KeyPair(clientCertPath, clientKeyPath)`) directly to the `tls.Config`:
   ```go
   client, err := softrainsrpc.NewClientFromCAMTLS(
       endpoint,
       serverName,
       "certs/root-ca.crt",
       "certs/detector-client.crt",
       "certs/detector-client.key",
       10*time.Second,
       64*1024,
   )
   ```
   Any unauthenticated client without a certificate signed by `clientCAFile` is rejected during the TLS handshake before a single RPC byte is read.

---

## Configuration Reference (`config/config.json`)

```json
{
  "defaultStartupProfile": "head-tracker",
  "connections": [
    {
      "name": "orangebot-rtsp",
      "source": "rtsp",
      "camera": {
        "url": "rtsp://192.168.68.67:8554/cam",
        "fov": 90,
        "width": 1280,
        "height": 720,
        "orientation": {
          "rotate": 180,
          "flipHorizontal": false,
          "flipVertical": false
        }
      },
      "cameraType": "standard",
      "control": {
        "url": "https://192.168.68.67:9444",
        "rpcEndpoint": "192.168.68.67:9444",
        "serverName": "softrainsvisions-servos-rpc3.local",
        "caCert": "certs/softrainsvision-servos-rpc2.local.crt",
        "protocol": "query-param",
        "pan": { "min": 20, "max": 120, "center": 90 }
      }
    }
  ],
  "startupProfiles": [
    {
      "name": "head-tracker",
      "botSetup": {
        "connectionName": "orangebot-rtsp",
        "aiName": "default",
        "tracking": {
          "panSensitivity": 0.15,
          "tiltSensitivity": 0.15,
          "deadZone": 30,
          "controlTimeoutMs": 30000,
          "controlRetryIntervalMs": 2000,
          "detectionTimeoutMs": 120000,
          "scanMode": {
            "enabled": true,
            "scanRange": 100,
            "scanStep": 10,
            "scanIntervalMs": 5000
          }
        }
      },
      "display": {
        "enabled": true,
        "windowTitle": "Head Tracker View",
        "frameInfoInterval": 2,
        "rotateDisplay": 180,
        "headless": false
      }
    }
  ]
}
```

---

## Building & Running

```bash
# Build binary into bin/softrains-detector
make build

# Validate config and run
make run
```
