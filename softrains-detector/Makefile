# Softrains AI Camera Makefile
# Build and development automation for the computer vision tracking system

# Application configuration
APP_NAME = softrains-detector
MAIN_FILE = cmd/main.go
BIN_DIR = bin
CONFIG_FILE = config/config.json

RUNTIME_IMAGE ?= softrainsvision-detector:latest
RUNTIME_PORT ?= 8443
RUNTIME_DOCKERFILE ?= docker/Dockerfile.softrainsvision-detector
K3S_CTR ?= sudo k3s ctr

# Go build configuration
GO_CMD = go
GO_BUILD = $(GO_CMD) build
GO_CLEAN = $(GO_CMD) clean
GO_TEST = $(GO_CMD) test
GO_GET = $(GO_CMD) get
GO_MOD = $(GO_CMD) mod
GO_VET = $(GO_CMD) vet
GO_FMT = $(GO_CMD) fmt

# Build flags
BUILD_FLAGS = -ldflags="-s -w"
BUILD_FLAGS_DEBUG = -race -gcflags="all=-N -l"

# Colors for output
RED = \033[31m
GREEN = \033[32m
YELLOW = \033[33m
BLUE = \033[34m
RESET = \033[0m

.PHONY: all build build-debug build-viewer clean test run dev check smoke fmt vet deps tidy help install-deps config-check start-runtime-8443 detector-image-build k3s-load-detector-image

# Default target
all: clean build

build:
	@echo "$(GREEN)Building $(APP_NAME) (socket-based Coral TPU)...$(RESET)"
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=1 $(GO_BUILD) $(BUILD_FLAGS) -o $(BIN_DIR)/$(APP_NAME) $(MAIN_FILE)
	@echo "$(GREEN)✓ Build complete: $(BIN_DIR)/$(APP_NAME)$(RESET)"



# Run the application with config validation
run: build config-check
	@echo "$(BLUE)Starting $(APP_NAME)...$(RESET)"
	./$(BIN_DIR)/$(APP_NAME)

# Development mode - build and run with debug info
dev: build-debug config-check
	@echo "$(BLUE)Starting $(APP_NAME) in development mode...$(RESET)"
	./$(BIN_DIR)/$(APP_NAME)-debug

# Run tests
test:
	@echo "$(BLUE)Running tests...$(RESET)"
	$(GO_TEST) -v ./...

# Run tests with coverage
test-coverage:
	@echo "$(BLUE)Running tests with coverage...$(RESET)"
	$(GO_TEST) -v -cover ./...
	@echo "$(GREEN)✓ Coverage report generated$(RESET)"

# Clean build artifacts
clean:
	@echo "$(RED)Cleaning build artifacts...$(RESET)"
	$(GO_CLEAN)
	rm -rf $(BIN_DIR)
	rm -f main
	rm -f $(APP_NAME)
	@echo "$(RED)✓ Clean complete$(RESET)"

# Format Go code
fmt:
	@echo "$(BLUE)Formatting Go code...$(RESET)"
	$(GO_FMT) ./...
	@echo "$(GREEN)✓ Code formatted$(RESET)"

# Run go vet for static analysis
vet:
	@echo "$(BLUE)Running go vet...$(RESET)"
	$(GO_VET) ./...
	@echo "$(GREEN)✓ Static analysis complete$(RESET)"

# Comprehensive code check (format, vet, test)
check: fmt vet test
	@echo "$(GREEN)✓ All checks passed$(RESET)"

# Install/update dependencies
deps:
	@echo "$(BLUE)Installing dependencies...$(RESET)"
	$(GO_GET) -u ./...
	$(GO_MOD) tidy
	@echo "$(GREEN)✓ Dependencies updated$(RESET)"

# Tidy go modules
tidy:
	@echo "$(BLUE)Tidying go modules...$(RESET)"
	$(GO_MOD) tidy
	@echo "$(GREEN)✓ Modules tidied$(RESET)"

# Install required development dependencies
install-deps:
	@echo "$(BLUE)Installing development dependencies...$(RESET)"
	$(GO_GET) gocv.io/x/gocv
	$(GO_GET) github.com/sirupsen/logrus
	$(GO_MOD) tidy
	@echo "$(GREEN)✓ Development dependencies installed$(RESET)"



# Check if configuration file exists and is valid JSON
config-check:
	@if [ ! -f $(CONFIG_FILE) ]; then \
		echo "$(RED)✗ Configuration file $(CONFIG_FILE) not found$(RESET)"; \
		exit 1; \
	fi
	@echo "$(GREEN)✓ Configuration file found$(RESET)"
	@python3 -m json.tool $(CONFIG_FILE) > /dev/null 2>&1 && \
		echo "$(GREEN)✓ Configuration file is valid JSON$(RESET)" || \
		(echo "$(RED)✗ Configuration file has invalid JSON$(RESET)" && exit 1)

# Create a release build with version info
release: clean check
	@echo "$(GREEN)Creating release build...$(RESET)"
	@mkdir -p $(BIN_DIR)
	$(GO_BUILD) $(BUILD_FLAGS) -o $(BIN_DIR)/$(APP_NAME) $(MAIN_FILE)
	@echo "$(GREEN)✓ Release build complete: $(BIN_DIR)/$(APP_NAME)$(RESET)"

# Quick development cycle (clean, check, build, run)
quick: clean check build run

detector-image-build:
	@if [ -f $(RUNTIME_DOCKERFILE) ]; then \
		echo "$(BLUE)Building detector image from $(RUNTIME_DOCKERFILE)...$(RESET)"; \
		docker build --no-cache -f $(RUNTIME_DOCKERFILE) -t $(RUNTIME_IMAGE) .; \
		echo "$(GREEN)✓ Detector image built: $(RUNTIME_IMAGE)$(RESET)"; \
	else \
		echo "$(RED)✗ $(RUNTIME_DOCKERFILE) not found$(RESET)"; \
		exit 1; \
	fi

k3s-load-detector-image: detector-image-build
	@echo "$(BLUE)Importing $(RUNTIME_IMAGE) into k3s containerd...$(RESET)"
	@docker save $(RUNTIME_IMAGE) | $(K3S_CTR) images import -
	@echo "$(GREEN)✓ Detector image imported into k3s containerd: $(RUNTIME_IMAGE)$(RESET)"

