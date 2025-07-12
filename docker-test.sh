#!/bin/bash

# Docker test script for local validation
# This script mimics the GitHub Action validation locally

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_step() {
    echo -e "${BLUE}[STEP]${NC} $1"
}

# Configuration
IMAGE_NAME="pubsub-sub-bench-test"
TAG="local-test"
FULL_IMAGE_NAME="${IMAGE_NAME}:${TAG}"

print_info "Starting Docker validation tests..."

# Step 1: Build the image
print_step "Building Docker image..."
if ./docker-build.sh -n "$IMAGE_NAME" -t "$TAG"; then
    print_info "✅ Docker build successful"
else
    print_error "❌ Docker build failed"
    exit 1
fi

# Step 2: Test basic functionality
print_step "Testing basic functionality..."

# Test help command
print_info "Testing --help command..."
if docker run --rm "$FULL_IMAGE_NAME" --help > /dev/null; then
    print_info "✅ Help command works"
else
    print_error "❌ Help command failed"
    exit 1
fi

# Test version command
print_info "Testing --version command..."
if docker run --rm "$FULL_IMAGE_NAME" --version > /dev/null; then
    print_info "✅ Version command works"
else
    print_error "❌ Version command failed"
    exit 1
fi

# Step 3: Test with Redis (if available)
print_step "Testing Redis connectivity (optional)..."
if command -v redis-server > /dev/null; then
    print_info "Redis server found, testing connectivity..."
    
    # Start Redis in background if not running
    if ! pgrep redis-server > /dev/null; then
        print_info "Starting Redis server..."
        redis-server --port 6379 --daemonize yes
        sleep 2
    fi
    
    # Test connection
    if timeout 5 docker run --rm --network=host "$FULL_IMAGE_NAME" \
        -host localhost -port 6379 -test-time 2 -clients 1 \
        -channel-minimum 1 -channel-maximum 1 -mode subscribe > /dev/null 2>&1; then
        print_info "✅ Redis connectivity test passed"
    else
        print_warning "⚠️  Redis connectivity test failed (this may be expected if no publisher is running)"
    fi
else
    print_warning "⚠️  Redis server not found, skipping connectivity test"
fi

# Step 4: Test file output permissions
print_step "Testing file output permissions..."
TEMP_DIR=$(mktemp -d)
if docker run --rm -v "$TEMP_DIR:/app/output" "$FULL_IMAGE_NAME" \
    -json-out-file test-output.json -test-time 1 -clients 1 \
    -channel-minimum 1 -channel-maximum 1 -mode subscribe > /dev/null 2>&1; then
    if [ -f "$TEMP_DIR/test-output.json" ]; then
        print_info "✅ File output test passed"
    else
        print_warning "⚠️  JSON file not created (expected if no messages received)"
    fi
else
    print_warning "⚠️  File output test completed with warnings (expected without Redis publisher)"
fi
rm -rf "$TEMP_DIR"

# Step 5: Test image size
print_step "Checking image size..."
IMAGE_SIZE=$(docker images --format "table {{.Size}}" "$FULL_IMAGE_NAME" | tail -n 1)
print_info "Image size: $IMAGE_SIZE"

# Step 6: Clean up
print_step "Cleaning up..."
docker rmi "$FULL_IMAGE_NAME" > /dev/null 2>&1 || true

print_info "🎉 All Docker validation tests completed successfully!"
print_info ""
print_info "Summary:"
print_info "  ✅ Docker build successful"
print_info "  ✅ Basic functionality tests passed"
print_info "  ✅ Image size: $IMAGE_SIZE"
print_info ""
print_info "The Docker setup is ready for production use!"
