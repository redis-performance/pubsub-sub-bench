#!/bin/bash

# Docker run script for pubsub-sub-bench
# This script provides convenient ways to run the benchmark in Docker

set -e

# Default values
IMAGE_NAME="pubsub-sub-bench:latest"
REDIS_HOST="localhost"
REDIS_PORT="6379"
MODE="subscribe"
CLIENTS="1"
NETWORK=""
EXTRA_ARGS=""

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

print_example() {
    echo -e "${BLUE}[EXAMPLE]${NC} $1"
}

# Function to show usage
usage() {
    echo "Usage: $0 [OPTIONS] [-- EXTRA_ARGS]"
    echo ""
    echo "Options:"
    echo "  -i, --image IMAGE     Docker image name (default: pubsub-sub-bench:latest)"
    echo "  -H, --host HOST       Redis host (default: localhost)"
    echo "  -p, --port PORT       Redis port (default: 6379)"
    echo "  -m, --mode MODE       Benchmark mode: subscribe|ssubscribe|publish|spublish (default: subscribe)"
    echo "  -c, --clients NUM     Number of clients (default: 1)"
    echo "  -n, --network NET     Docker network to use"
    echo "  -h, --help            Show this help message"
    echo ""
    echo "Examples:"
    print_example "$0                                    # Run with defaults (help)"
    print_example "$0 -H redis -m subscribe -c 10       # Subscribe mode with 10 clients"
    print_example "$0 -H redis -m publish -c 5          # Publish mode with 5 clients"
    print_example "$0 -n redis-net -H redis-server      # Use custom network"
    print_example "$0 -- --verbose --test-time 60       # Pass extra arguments"
    echo ""
    echo "Common Redis setups:"
    print_example "# Local Redis:"
    print_example "$0 -H host.docker.internal"
    echo ""
    print_example "# Redis in Docker network:"
    print_example "$0 -n redis-network -H redis-container"
    echo ""
    print_example "# Redis Cluster:"
    print_example "$0 -H redis-cluster-node1 -p 7000 -- --cluster-mode"
    echo ""
    print_example "# With JSON output (mount current directory):"
    print_example "docker run --rm -v \$(pwd):/app/output --network host filipe958/pubsub-sub-bench:latest -json-out-file results.json -host localhost -mode subscribe"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -i|--image)
            IMAGE_NAME="$2"
            shift 2
            ;;
        -H|--host)
            REDIS_HOST="$2"
            shift 2
            ;;
        -p|--port)
            REDIS_PORT="$2"
            shift 2
            ;;
        -m|--mode)
            MODE="$2"
            shift 2
            ;;
        -c|--clients)
            CLIENTS="$2"
            shift 2
            ;;
        -n|--network)
            NETWORK="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        --)
            shift
            EXTRA_ARGS="$*"
            break
            ;;
        *)
            print_error "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# Build Docker run command
DOCKER_CMD="docker run --rm -it"

# Add network if specified
if [[ -n "$NETWORK" ]]; then
    DOCKER_CMD="$DOCKER_CMD --network $NETWORK"
    print_info "Using Docker network: $NETWORK"
fi

# Add image
DOCKER_CMD="$DOCKER_CMD $IMAGE_NAME"

# If no extra args provided, show help
if [[ -z "$EXTRA_ARGS" && "$MODE" == "subscribe" && "$CLIENTS" == "1" && "$REDIS_HOST" == "localhost" ]]; then
    print_info "No specific configuration provided, showing help:"
    DOCKER_CMD="$DOCKER_CMD --help"
else
    # Add benchmark arguments
    DOCKER_CMD="$DOCKER_CMD --host $REDIS_HOST --port $REDIS_PORT --mode $MODE --clients $CLIENTS"
    
    # Add extra arguments if provided
    if [[ -n "$EXTRA_ARGS" ]]; then
        DOCKER_CMD="$DOCKER_CMD $EXTRA_ARGS"
    fi
    
    print_info "Configuration:"
    print_info "  Redis: $REDIS_HOST:$REDIS_PORT"
    print_info "  Mode: $MODE"
    print_info "  Clients: $CLIENTS"
    if [[ -n "$EXTRA_ARGS" ]]; then
        print_info "  Extra args: $EXTRA_ARGS"
    fi
fi

print_info "Executing: $DOCKER_CMD"
echo ""

# Execute the command
exec $DOCKER_CMD
