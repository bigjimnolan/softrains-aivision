# Coral TPU CGO Integration

The C++ Coral detector has been integrated into the Go code via CGO (C/Go language bindings).

## What Changed

### New Files
- `cpp/src/coral_wrapper.h` - C interface to CoralDetector C++ class
- `cpp/src/coral_wrapper.c` - C implementation wrapping C++ detector
- `pkg/softrainsvision/coral_cgo.go` - Go CGO bindings

### Updated Files
- `cpp/BUILD.bazel` - Added coral_wrapper.c and coral_wrapper.h to library build
- `pkg/softrainsvision/detector.go` - Added CGO detector support:
  - `NewHumanDetectorWithCoralTPUCGO()` - New constructor
  - `detectHumansCGO()` - New detection method using CGO
  - Updated `Close()`, `SetConfidenceThreshold()`, `SetNMSThreshold()`
- `pkg/softrainsvision/types.go` - Added `DetectionResult` struct

## Usage in Your Code

### Option 1: Use the new CGO-based detector (Recommended)

```go
import "path/to/softrainsvision"

// Create detector with CGO bridge
detector, err := softrainsvision.NewHumanDetectorWithCoralTPUCGO(
    "models/ssd_mobilenet_v2_coco_quant_postprocess_edgetpu.tflite",
    640, // frameWidth
    480, // frameHeight
    logger,
)
if err != nil {
    log.Fatal("Failed to create detector:", err)
}
defer detector.Close()

// Run detection on a frame
detections, err := detector.DetectHumans(frame)
if err != nil {
    log.Error("Detection failed:", err)
}

// Use detections
for _, det := range detections {
    fmt.Printf("Detection: %+v\n", det)
}
```

### Option 2: Keep existing CPU detector

```go
// Use the original CPU-based detector (unchanged behavior)
detector, err := softrainsvision.NewHumanDetector(
    "models/yolo.weights",
    "models/yolo.cfg",
    640, 480,
    logger,
)
```

## Build Requirements

### For Go Build with CGO
You need:
1. `libcoral` development libraries (libedgetpu)
2. `libusb-1.0-dev` for USB access
3. C++ compiler (g++ or clang++)
4. TensorFlow Lite headers (from libcoral)

Install on Ubuntu:
```bash
echo "deb https://packages.cloud.google.com/apt coral-edgetpu-stable main" | \
  sudo tee /etc/apt/sources.list.d/coral-edgetpu.list

curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | sudo apt-key add -

sudo apt-get update
sudo apt-get install -y libedgetpu-dev libusb-1.0-0-dev
```

### Bazel Build (C++ Library)

The C++ library is still built with Bazel:
```bash
cd cpp
bazel build //:softrains_coral_detector
```

### Go Build with CGO

```bash
# Linux/macOS
CGO_ENABLED=1 go build -ldflags="-L/usr/lib/x86_64-linux-gnu -lstdc++" ./cmd/main.go

# Or with pkg-config
PKG_CONFIG_PATH=/usr/lib/x86_64-linux-gnu/pkgconfig CGO_ENABLED=1 go build ./cmd/main.go
```

## Docker Deployment

To use in Docker:

1. **Build image** must have libedgetpu and dependencies:
```dockerfile
FROM ubuntu:20.04

RUN echo "deb https://packages.cloud.google.com/apt coral-edgetpu-stable main" | \
    tee /etc/apt/sources.list.d/coral-edgetpu.list && \
    curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | apt-key add - && \
    apt-get update && \
    apt-get install -y libedgetpu-dev libusb-1.0-0-dev g++ && \
    rm -rf /var/lib/apt/lists/*
```

2. **Runtime** must pass TPU device:
```bash
docker run --rm \
  --device=/dev/bus/usb \
  --privileged \
  -v /models:/models \
  your-image:tag
```

## Model Format

For `NewHumanDetectorWithCoralTPUCGO()`:
- **Input**: TensorFlow Lite model compiled for Edge TPU
- **Extension**: `.tflite`
- **Format**: 300x300 (for SSD MobileNet), 3 channels (RGB or BGR)
- **Must be** compiled with `edgetpu_compiler`

## Performance Notes

- **Inference**: ~10-50ms per frame (vs 100-200ms CPU)
- **FPS**: 20-100 fps (vs 5-10 fps CPU)
- **Requires**: Coral USB Accelerator or Coral Dev Board connected via USB

## Testing

Test the integration:

```bash
# In your code
detector, err := softrainsvision.NewHumanDetectorWithCoralTPUCGO(
    "path/to/model.tflite", 640, 480, logger)

// Or test via bazel
cd cpp
./bazel-bin/test_detector path/to/model.tflite path/to/test.rgb
```

## Troubleshooting

### "Failed to create Coral TPU detector"
- Verify TPU is connected: `lsusb | grep 1a6e`
- Inside container: ensure `--device=/dev/bus/usb` is passed
- Check libedgetpu is installed: `ldconfig -p | grep edgetpu`

### "Invalid image size"
- Ensure RGB image is 300x300x3 bytes
- Convert test images: `convert input.bmp -resize 300x300! -depth 8 rgb:output.rgb`

### CGO compilation errors
- Check `cgo` is enabled: `CGO_ENABLED=1`
- Verify libcoral headers are in include path
- Check C++ library link flags
