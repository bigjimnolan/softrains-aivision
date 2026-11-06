# Model Configuration Guide

This guide explains how to configure different detection models with softrains-detector using the new configurable settings.

## Configuration Structure

The `config.json` now includes comprehensive model settings that allow you to easily switch between different models without code changes.

### Key Configuration Fields

```json
"ai": {
  "modelType": "ssd",                    // Model type: "yolo" or "ssd"
  "modelPath": "./models/yolo.weights",  // CPU model path (for YOLO)
  "configPath": "./models/yolo.cfg",     // Config path (for YOLO)
  "tpuModelPath": "./models/model_edgetpu.tflite",  // Edge TPU model
  "inputWidth": 384,                     // Model input width
  "inputHeight": 384,                    // Model input height
  "confidenceThreshold": 0.5,            // Detection confidence threshold
  "nmsThreshold": 0.4,                   // Non-Maximum Suppression threshold
  "processEveryNFrames": 2,              // Process every Nth frame
  "useCoralTPU": true,                   // Use Coral TPU if available
  "detectionFilters": {
    "minBoxWidth": 20,                   // Minimum detection width (pixels)
    "minBoxHeight": 40,                  // Minimum detection height (pixels)
    "minBoxArea": 400,                   // Minimum detection area (pixels²)
    "maxAreaRatio": 0.67,                // Max area as ratio of frame
    "maxWidthRatio": 0.9,                // Max width as ratio of frame
    "maxHeightRatio": 0.95               // Max height as ratio of frame
  }
}
```

## Configuration Examples

### Example 1: YOLO on CPU

```json
{
  "ai": {
    "modelType": "yolo",
    "modelPath": "./models/yolo.weights",
    "configPath": "./models/yolo.cfg",
    "inputWidth": 416,
    "inputHeight": 416,
    "confidenceThreshold": 0.5,
    "nmsThreshold": 0.4,
    "useCoralTPU": false,
    "detectionFilters": {
      "minBoxWidth": 20,
      "minBoxHeight": 40,
      "minBoxArea": 400,
      "maxAreaRatio": 0.67,
      "maxWidthRatio": 0.9,
      "maxHeightRatio": 0.95
    }
  }
}
```

### Example 2: EfficientDet-Lite1 on Coral TPU

```json
{
  "ai": {
    "modelType": "ssd",
    "modelPath": "./models/efficientdet-lite1.tflite",
    "configPath": "",
    "tpuModelPath": "./models/efficientdet-lite1_edgetpu.tflite",
    "inputWidth": 384,
    "inputHeight": 384,
    "confidenceThreshold": 0.5,
    "nmsThreshold": 0.4,
    "useCoralTPU": true,
    "detectionFilters": {
      "minBoxWidth": 20,
      "minBoxHeight": 40,
      "minBoxArea": 400,
      "maxAreaRatio": 0.67,
      "maxWidthRatio": 0.9,
      "maxHeightRatio": 0.95
    }
  }
}
```

### Example 3: SSD MobileNet V2 (300x300) on Coral TPU

```json
{
  "ai": {
    "modelType": "ssd",
    "modelPath": "./models/ssdlite_mobilenet_v2.tflite",
    "configPath": "",
    "tpuModelPath": "./models/ssdlite_mobilenet_v2_edgetpu.tflite",
    "inputWidth": 300,
    "inputHeight": 300,
    "confidenceThreshold": 0.5,
    "nmsThreshold": 0.4,
    "useCoralTPU": true,
    "detectionFilters": {
      "minBoxWidth": 15,
      "minBoxHeight": 30,
      "minBoxArea": 400,
      "maxAreaRatio": 0.67,
      "maxWidthRatio": 0.9,
      "maxHeightRatio": 0.95
    }
  }
}
```

## Parameter Descriptions

### Model Configuration

- **modelType**: Determines output parsing strategy
  - `"yolo"`: YOLO format output (center_x, center_y, width, height)
  - `"ssd"`: SSD format output (normalized coordinates)

- **inputWidth / inputHeight**: The dimensions your model expects
  - YOLO: typically 416 or 608
  - EfficientDet-Lite: 320, 384, 448, 512, or 640
  - SSD MobileNet: 300 or 320

### Detection Thresholds

- **confidenceThreshold** (0.0-1.0): 
  - Lower = more detections (higher false positives)
  - Higher = fewer, more confident detections
  - Typical: 0.4-0.6

- **nmsThreshold** (0.0-1.0):
  - Lower = more aggressive filtering (fewer overlapping boxes)
  - Higher = allows more overlapping detections
  - Typical: 0.3-0.5

### Detection Filters

Used to reduce false positives by filtering unrealistic detections:

- **minBoxWidth/Height**: Minimum detection size (eliminates tiny noise)
- **minBoxArea**: Minimum detection area in pixels
- **maxAreaRatio**: Detections can't exceed this % of frame area
- **maxWidthRatio**: Detections can't exceed this % of frame width
- **maxHeightRatio**: Detections can't exceed this % of frame height

## Recommended Settings by Model

### EfficientDet-Lite1 (384x384) - Recommended for Coral TPU

```json
{
  "modelType": "ssd",
  "inputWidth": 384,
  "inputHeight": 384,
  "confidenceThreshold": 0.5,
  "nmsThreshold": 0.4,
  "detectionFilters": {
    "minBoxWidth": 20,
    "minBoxHeight": 40,
    "minBoxArea": 400
  }
}
```

**Pros**: Good balance of speed (56ms) and accuracy (34.3% mAP)  
**Typical use**: Real-time human detection with moderate computing power

### SSD MobileNet V2 (300x300)

```json
{
  "modelType": "ssd",
  "inputWidth": 300,
  "inputHeight": 300,
  "confidenceThreshold": 0.5,
  "nmsThreshold": 0.4,
  "detectionFilters": {
    "minBoxWidth": 15,
    "minBoxHeight": 30,
    "minBoxArea": 300
  }
}
```

**Pros**: Very fast, smaller model size  
**Cons**: Lower accuracy (22-25% mAP)  
**Typical use**: Edge devices with limited resources

### EfficientDet-Lite3x (640x640)

```json
{
  "modelType": "ssd",
  "inputWidth": 640,
  "inputHeight": 640,
  "confidenceThreshold": 0.5,
  "nmsThreshold": 0.4,
  "detectionFilters": {
    "minBoxWidth": 30,
    "minBoxHeight": 50,
    "minBoxArea": 500
  }
}
```

**Pros**: Highest accuracy (43.9% mAP)  
**Cons**: Slower (197ms on Edge TPU)  
**Typical use**: When accuracy is prioritized over speed

## Tuning Tips

### To Reduce False Positives

1. Increase `confidenceThreshold` (0.5 → 0.7)
2. Increase `nmsThreshold` (0.4 → 0.5)
3. Increase `minBoxArea`
4. Lower `maxAreaRatio` and `maxWidthRatio`

### To Increase Detection Sensitivity

1. Decrease `confidenceThreshold` (0.5 → 0.3)
2. Decrease `minBoxArea`
3. Increase `maxAreaRatio`
4. Decrease `nmsThreshold`

### Performance Considerations

- **Accuracy vs Speed tradeoff**: Larger models (384+x384+) are more accurate but slower
- **Frame processing**: Lower `processEveryNFrames` for real-time, higher for reduced load
- **Coral TPU**: Always faster than CPU; use when available
- **Memory**: Smaller models use less memory; important for embedded systems

## Environment Variables

You can override the `useCoralTPU` setting with an environment variable:

```bash
USE_CORAL_TPU=true ./softrains-detector
USE_CORAL_TPU=false ./softrains-detector
```

## Switching Models

To switch from one model to another:

1. Update the configuration in `config.json`:
   - Set `modelType` to `"yolo"` or `"ssd"`
   - Update `inputWidth` and `inputHeight`
   - Set `tpuModelPath` to your new model file
   - Adjust `detectionFilters` if needed

2. No code recompilation required - restart softrains-detector with the updated config

## Model Download Sources

- **Coral Pre-built Models**: https://coral.ai/models/
- **Google TensorFlow Hub**: https://tfhub.dev/
- **ONNX Model Zoo**: https://github.com/onnx/models

## Troubleshooting

### Model not loading
- Verify file path is relative to where softrains-detector runs
- Check model format matches configuration (YOLO vs SSD)
- Ensure model is compiled for Edge TPU if using TPU

### Poor detection accuracy
- Check `confidenceThreshold` isn't too high
- Verify `detectionFilters` aren't too restrictive
- Try a different model if current one isn't suitable

### Slow inference
- Reduce `inputWidth`/`inputHeight` if possible
- Use Coral TPU instead of CPU
- Increase `processEveryNFrames` if real-time isn't required
