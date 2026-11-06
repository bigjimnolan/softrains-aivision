# Coral Edge TPU Setup Guide

A focused guide for setting up and using Coral Edge TPU with softrains-detector.

---

## 1. Host Setup

### Hardware Requirements
- **Coral USB Accelerator** or **Coral Dev Board**
- USB 3.0 port (USB 2.0 works but slower)
- 4GB+ RAM recommended

### Plug In and Verify

1. **Connect the Coral TPU** to your USB port

2. **Verify device is detected:**
```bash
lsusb | grep Google
# Expected output: Bus 001 Device 005: ID 1a6e:089a Google Inc.
```

3. **Check device permissions:**
```bash
ls -l /dev/bus/usb/*/
# Look for your Coral device - should be accessible
```

### Install Runtime Libraries

#### Ubuntu/Debian:
```bash
echo "deb https://packages.cloud.google.com/apt coral-edgetpu-stable main" | \
  sudo tee /etc/apt/sources.list.d/coral-edgetpu.list

curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | sudo apt-key add -

sudo apt-get update
sudo apt-get install -y libedgetpu1-std
```

#### macOS:
```bash
brew tap google-coral/homebrew-coral
brew install libcoral
```

### Set Up udev Rules (Linux)

Create `/etc/udev/rules.d/99-coral.rules`:
```bash
SUBSYSTEM=="usb", ATTRS{idVendor}=="1a6e", ATTRS{idProduct}=="089a", MODE="0664", GROUP="plugdev"
SUBSYSTEM=="usb", ATTRS{idVendor}=="18d1", ATTRS{idProduct}=="9302", MODE="0664", GROUP="plugdev"
```

Apply rules:
```bash
sudo udevadm control --reload-rules
sudo udevadm trigger
```

Add your user to plugdev group:
```bash
sudo usermod -a -G plugdev $USER
# Log out and back in for this to take effect
```

### Verify Installation

```bash
# Check if libraries are installed
ldconfig -p | grep edgetpu

# Should show: libedgetpu.so.1 (libc6,x86-64) => /usr/lib/x86_64-linux-gnu/libedgetpu.so.1
```

---

## 2. Docker/Kubernetes Integration

### Docker Container Access

To use the Coral TPU from inside a Docker container, you must pass through the USB device.

#### Basic Docker Run
```bash
docker run --rm -it \
  --device=/dev/bus/usb \
  your-image:tag
```

#### Docker Compose
```yaml
version: '3.8'
services:
  softrains-detector:
    image: softrains-detector:latest
    devices:
      - /dev/bus/usb:/dev/bus/usb
    privileged: false  # Not needed if device passthrough works
```

#### Dockerfile Setup

Your container needs the Edge TPU runtime:

```dockerfile
FROM ubuntu:20.04

# Install Edge TPU runtime
RUN echo "deb https://packages.cloud.google.com/apt coral-edgetpu-stable main" | \
    tee /etc/apt/sources.list.d/coral-edgetpu.list && \
    curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | apt-key add - && \
    apt-get update && \
    apt-get install -y libedgetpu1-std && \
    rm -rf /var/lib/apt/lists/*

# Copy your application
COPY . /app
WORKDIR /app
```

### Kubernetes

Use a Device Plugin or direct device mounting:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: softrains-detector
spec:
  containers:
  - name: inference
    image: softrains-detector:latest
    securityContext:
      privileged: true  # Required for USB access
    volumeMounts:
    - name: usb
      mountPath: /dev/bus/usb
  volumes:
  - name: usb
    hostPath:
      path: /dev/bus/usb
```

**Note:** For production, use a proper [USB Device Plugin](https://github.com/squat/generic-device-plugin) instead of privileged mode.

### Testing in Container

```bash
# Shell into your container
docker run --rm -it \
  --device=/dev/bus/usb \
  your-image:tag \
  /bin/bash

# Inside container, verify TPU access
lsusb | grep Google
ls /dev/bus/usb/
```

---

## 3. Code Configuration

### Model Requirements
- **Format:** TensorFlow Lite (`.tflite`)
- **Compilation:** Must be compiled for Edge TPU
- **Location:** Place in `/models/` or configured path

### Usage in Go Code

#### Option 1: Explicit TPU Initialization
```go
detector, err := softrainsvision.NewHumanDetectorWithCoralTPU(
    "models/efficientdet_lite1_384_ptq_edgetpu.tflite",
    640,
    480,
    logger,
)
```

#### Option 2: Auto-Select with Environment Variable
```go
// Reads USE_CORAL_TPU environment variable
device := softrainsvision.DeviceCPU
if os.Getenv("USE_CORAL_TPU") == "true" {
    device = softrainsvision.DeviceCoralTPU
}

detector, err := softrainsvision.NewHumanDetectorAuto(
    device,
    "models/model_edgetpu.tflite",  // TPU model
    "models/yolo.cfg",               // CPU config (ignored if TPU)
    640,
    480,
    logger,
)
```

### Configuration File

Add to your `config.json`:
```json
{
  "ai": {
    "useCoralTPU": true,
    "tpuModelPath": "models/efficientdet_lite1_384_ptq_edgetpu.tflite",
    "modelPath": "models/yolo.weights",
    "configPath": "models/yolo.cfg",
    "inputSize": 640
  }
}
```

### Environment Variables

```bash
# Enable TPU
export USE_CORAL_TPU=true

# Run your application
./softrains-detector

# Or inline
USE_CORAL_TPU=true ./softrains-detector
```

### Docker Environment
```bash
docker run --rm \
  --device=/dev/bus/usb \
  -e USE_CORAL_TPU=true \
  -v $(pwd)/models:/models \
  softrains-detector:latest
```

---

## Troubleshooting

### Device Not Found
```bash
# Check USB connection
lsusb | grep Google

# Check permissions
ls -l /dev/bus/usb/*/*

# Restart udev
sudo udevadm control --reload-rules
sudo udevadm trigger
```

### "Could not create Edge TPU delegate"
- Verify libedgetpu is installed: `ldconfig -p | grep edgetpu`
- Check device permissions
- Try with `sudo` to rule out permission issues
- Ensure only one process is using the TPU

### Docker Container Can't Access TPU
- Verify `--device=/dev/bus/usb` is passed
- Check container has libedgetpu installed
- Run `lsusb` inside container to verify passthrough

### Multiple Coral Devices
```bash
# List all Edge TPU devices
python3 -c "from pycoral.utils import edgetpu; print(edgetpu.list_edge_tpus())"
```

---

## Performance Notes

| Hardware | Inference Time | FPS |
|----------|---------------|-----|
| CPU (i7) | 100-200ms | 5-10 |
| Coral USB | 10-50ms | 20-100 |

---

## References
- [Coral Documentation](https://coral.ai/docs/)
- [libedgetpu on GitHub](https://github.com/google-coral/libedgetpu)
