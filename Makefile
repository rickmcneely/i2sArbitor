.PHONY: build clean install uninstall run deps

BINARY_NAME=i2sarbitor
BUILD_DIR=.
INSTALL_DIR=/usr/local/bin
CONFIG_DIR=/etc/i2sarbitor
SYSTEMD_DIR=/etc/systemd/system

# Build the binary
build: deps
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/i2sarbitor

# Build for Raspberry Pi (ARM64)
build-pi: deps
	GOOS=linux GOARCH=arm64 go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/i2sarbitor

# Build for Raspberry Pi (ARM 32-bit)
build-pi32: deps
	GOOS=linux GOARCH=arm GOARM=7 go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/i2sarbitor

# Download dependencies
deps:
	go mod download
	go mod tidy

# Run locally
run: build
	./$(BINARY_NAME)

# Install the service
install: build
	@echo "Installing $(BINARY_NAME)..."
	sudo cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/
	sudo mkdir -p $(CONFIG_DIR)
	sudo cp configs/i2sarbitor.yaml $(CONFIG_DIR)/
	sudo cp systemd/i2sarbitor.service $(SYSTEMD_DIR)/
	sudo systemctl daemon-reload
	sudo systemctl enable i2sarbitor
	@echo "Installation complete. Start with: sudo systemctl start i2sarbitor"

# Uninstall the service
uninstall:
	@echo "Uninstalling $(BINARY_NAME)..."
	-sudo systemctl stop i2sarbitor
	-sudo systemctl disable i2sarbitor
	-sudo rm -f $(SYSTEMD_DIR)/i2sarbitor.service
	-sudo rm -f $(INSTALL_DIR)/$(BINARY_NAME)
	-sudo rm -rf $(CONFIG_DIR)
	sudo systemctl daemon-reload
	@echo "Uninstallation complete."

# Clean build artifacts
clean:
	rm -f $(BUILD_DIR)/$(BINARY_NAME)
	go clean

# Show help
help:
	@echo "Available targets:"
	@echo "  build      - Build the binary for current platform"
	@echo "  build-pi   - Build for Raspberry Pi (ARM64)"
	@echo "  build-pi32 - Build for Raspberry Pi (ARM 32-bit)"
	@echo "  deps       - Download and tidy dependencies"
	@echo "  run        - Build and run locally"
	@echo "  install    - Install as system service"
	@echo "  uninstall  - Remove system service"
	@echo "  clean      - Clean build artifacts"
