BINARY   := gimbalrpc
CMD      := ./cmd/gimbalrpc
RAG_BINARY := ragindexer
RAG_CMD    := ./cmd/ragindexer
OUT_DIR  := bin
SERVO_BINARY := gpio-servo
TEST_CMD    := ./cmd/gpio-servo
ORANGE_HOST  := orangebot
ORANGE_PATH  := /home/bigjim/new-arm64
ORANGE_TEST_PATH := /home/bigjim/test-arm64
# ARM64 Armbian target
GOOS   := linux
GOARCH := arm64

.PHONY: all build build-servo clean tidy orange orange-servo

all: build

build: tidy
	CGO_CFLAGS="-I/home/bigjim/stt_workspace/vosk-libs" \
	CGO_LDFLAGS="-L/home/bigjim/stt_workspace/vosk-libs -lvosk -lportaudio" \
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(OUT_DIR)/$(BINARY)-arm64 $(CMD)
	@echo "Built $(OUT_DIR)/$(BINARY)-arm64"
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(OUT_DIR)/$(RAG_BINARY)-arm64 $(RAG_CMD)
	@echo "Built $(OUT_DIR)/$(RAG_BINARY)-arm64"

build-servo: tidy
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(OUT_DIR)/$(SERVO_BINARY)-arm64 $(TEST_CMD)
	@echo "Built $(OUT_DIR)/$(SERVO_BINARY)-arm64"

tidy:
	go mod tidy

clean:
	rm -f $(OUT_DIR)/$(BINARY)-arm64 $(OUT_DIR)/$(RAG_BINARY)-arm64 $(OUT_DIR)/$(SERVO_BINARY)-arm64

orange: clean build
	scp $(OUT_DIR)/$(BINARY)-arm64 $(ORANGE_HOST):$(ORANGE_PATH)

orange-test: build-servo
	scp $(OUT_DIR)/$(SERVO_BINARY)-arm64 $(ORANGE_HOST):$(ORANGE_TEST_PATH)
test: tidy
	go run test/testrpc.go

	
