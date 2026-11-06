# Softrains TPU Docker Build Guide

This guide captures the working Docker-based build and run flow for libcoral and the Softrains C++ project.

## Overview

We use a cached Docker image to avoid re-downloading TensorFlow every build. The image also includes the EdgeTPU runtime so TPU access works inside the container.

## 1) Build the Docker images (libcoral)

From the libcoral repo:

```bash
cd /home/bigjim/Repos/external/libcoral

# Build base image only (optional)
./build-cached-image.sh base

# Build cached image with TensorFlow downloaded
./build-cached-image.sh bazel-cached

# Optional: pre-compile one example (bigger image)
./build-cached-image.sh bazel-prebuilt
```

Images created:
- `coral-edgetpu-ubuntu-18.04` (base)
- `coral-edgetpu-ubuntu-18.04-cached` (deps cached)
- `coral-edgetpu-ubuntu-18.04-prebuilt` (deps + prebuilt example)

## 2) Build libcoral examples using the cached image

```bash
cd /home/bigjim/Repos/external/libcoral
./docker-build-cached.sh examples k8
```

Outputs:
- `/home/bigjim/Repos/external/libcoral/out/k8/examples/`

## 3) Run `classify_image` inside the container (TPU)

Use a `.rgb` test image that exists in `test_data` (for example `bird.rgb`).

```bash
docker run --rm -it \
  -v /home/bigjim/Repos/external/libcoral:/workspace \
  --device=/dev/bus/usb \
  coral-edgetpu-ubuntu-18.04-cached \
  /bin/bash -c "
    ./out/k8/examples/classify_image \
      --image_path=/workspace/test_data/bird.rgb \
      --model_path=/workspace/test_data/mobilenet_v1_1.0_224_quant_edgetpu.tflite \
      --labels_path=/workspace/test_data/imagenet_labels.txt
  "
```

If you want to use a BMP or JPG, convert to raw RGB first.

## 4) Build Softrains C++ project using the cached image

From the Softrains C++ folder:

```bash
cd /home/bigjim/Repos/softrains-detector-consumer/softrainsaivsion/softrains-detector/cpp

# Optional: override libcoral repo location
# export LIBCORAL_DIR=/home/bigjim/Repos/external/libcoral

./docker-build.sh
```

This script:
- Selects the best available Docker image (cached if present)
- Mounts the project and libcoral repositories
- Runs `bazel build //...` inside the container

## 5) TPU access inside Docker

If your runtime needs TPU access, run containers with:

```bash
--device=/dev/bus/usb
```

This exposes the USB TPU to the container.

## Notes

- `docker-build-cached.sh` uses the image's internal Bazel cache. Do not mount a host Bazel cache there, or it will override the image cache and force downloads again.
- If you want to rebuild everything from scratch, remove the Docker images and rebuild with `build-cached-image.sh`.
