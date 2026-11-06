# YOLO to TFLite Conversion Guide

This Dockerfile makes it easy to convert YOLO models to TensorFlow Lite format optimized for Google Coral TPU from anywhere.

## Prerequisites

- Docker installed and running
- Your YOLO model files: `yolo.weights` and `yolo.cfg`

## Quick Start

### 1. Build the Docker Image

```bash
docker build -f Dockerfile.tflite -t yolo-tflite .
```

### 2. Convert Your Model

#### Option A: Using the helper script (recommended)

```bash
docker run -v /path/to/your/models:/models yolo-tflite \
    python3 convert_yolo_to_tflite.py \
    --weights yolo.weights \
    --config yolo.cfg \
    --output yolo.tflite \
    --compile-edgetpu
```

#### Option B: Manual step-by-step conversion

**Step 1: YOLO to ONNX**
```bash
docker run -v /path/to/your/models:/models yolo-tflite \
    python3 -m tf2onnx.convert --yolo \
    --weights yolo.weights \
    --config yolo.cfg \
    --output_file yolo.onnx
```

**Step 2: ONNX to TFLite** (create `convert.py` in your models directory)
```python
import tensorflow as tf

converter = tf.lite.TFLiteConverter.from_saved_model("saved_model")
tflite_model = converter.convert()

with open("yolo.tflite", "wb") as f:
    f.write(tflite_model)
```

```bash
docker run -v /path/to/your/models:/models yolo-tflite python3 convert.py
```

**Step 3: Compile for Edge TPU**
```bash
docker run -v /path/to/your/models:/models yolo-tflite \
    edgetpu_compiler yolo.tflite -o yolo_edgetpu.tflite
```

## Example

```bash
# Assuming your models are in ~/models/
docker run -v ~/models:/models yolo-tflite \
    python3 convert_yolo_to_tflite.py \
    --weights yolo.weights \
    --config yolo.cfg \
    --output yolo.tflite \
    --compile-edgetpu --keep-intermediates
```

After completion, check `~/models/` for:
- `yolo.tflite` - TensorFlow Lite model
- `yolo_edgetpu.tflite` - Edge TPU optimized model (if `--compile-edgetpu` used)
- `yolo.onnx` - ONNX intermediate file (if `--keep-intermediates` used)

## Container Contents

The Docker image includes:
- **Python 3.10**
- **TensorFlow 2.12+** - For model conversion
- **tf2onnx** - YOLO format conversion
- **ONNX** - Intermediate format support
- **Edge TPU Compiler** - For TPU optimization

## Advanced Usage

### Use interactive Python shell

```bash
docker run -it -v /path/to/your/models:/models yolo-tflite python3
```

### Run a custom Python script

```bash
docker run -v /path/to/your/models:/models yolo-tflite \
    python3 /models/your_script.py
```

### Access bash shell

```bash
docker run -it -v /path/to/your/models:/models yolo-tflite /bin/bash
```

## Troubleshooting

### "Could not find tf2onnx"
- Ensure you're running the command inside the docker container
- Check that the image was built successfully

### "edgetpu_compiler: command not found"
- Rebuild the image: `docker build -f Dockerfile.tflite -t yolo-tflite .`
- Verify internet connection for Coral package download

### Converting large models
- Increase Docker memory: `docker run --memory 4g ...`
- Use `--swap` flag if needed: `docker run --memory 4g --memory-swap 8g ...`

## Performance Tips

1. **Quantization**: The resulting TFLite models are quantized by default for Edge TPU compatibility
2. **Model Size**: TFLite models are significantly smaller than original YOLO weights
3. **Inference Speed**: Coral TPU models run 10-50x faster than CPU inference

## For Integration with softrains-detector

Once converted, use the TFLite model with softrains-detector:

```go
detector, err := softrainsvision.NewHumanDetectorWithCoralTPU(
    "path/to/yolo_edgetpu.tflite",  // Use the Edge TPU compiled model
    width,
    height,
    logger,
)
```

See [CORAL_TPU_SETUP.md](CORAL_TPU_SETUP.md) for full integration details.
