# Docker Setup and Publishing Guide

This guide explains how to set up Docker publishing for the `pubsub-sub-bench` project to Docker Hub repository `filipe958/pubsub-sub-bench`.

## 🔐 Required GitHub Secrets

To enable automated Docker publishing, you need to configure the following secrets in your GitHub repository:

### Setting up Docker Hub Secrets

1. **Go to your GitHub repository** → Settings → Secrets and variables → Actions

2. **Add the following repository secrets:**

   - **`DOCKER_USERNAME`**: Your Docker Hub username (`filipe958`)
   - **`DOCKER_PASSWORD`**: Your Docker Hub access token (NOT your password)

### Creating a Docker Hub Access Token

1. **Log in to Docker Hub** at https://hub.docker.com
2. **Go to Account Settings** → Security → Access Tokens
3. **Click "New Access Token"**
4. **Configure the token:**
   - Name: `GitHub Actions - pubsub-sub-bench`
   - Permissions: `Read, Write, Delete`
5. **Copy the generated token** and use it as `DOCKER_PASSWORD` secret

⚠️ **Important**: Use an access token, not your Docker Hub password, for better security.

## 🚀 Automated Publishing

Once secrets are configured, Docker images will be automatically published:

### Master Branch Commits
- **Trigger**: Every push to `master` branch
- **Tags**: `latest`, `master-{sha}`, `master-{timestamp}`
- **Platforms**: `linux/amd64`, `linux/arm64`

### Releases
- **Trigger**: When a GitHub release is published
- **Tags**: `{version}`, `{major}.{minor}`, `{major}`, `latest`
- **Platforms**: `linux/amd64`, `linux/arm64`
- **Security**: Includes Trivy vulnerability scanning

### Example Tags for Release v1.2.3
```
filipe958/pubsub-sub-bench:v1.2.3
filipe958/pubsub-sub-bench:1.2.3
filipe958/pubsub-sub-bench:1.2
filipe958/pubsub-sub-bench:1
filipe958/pubsub-sub-bench:latest
```

## 🛠️ Manual Building and Publishing

### Local Build
```bash
# Build only
./docker-build.sh

# Build with custom tag
./docker-build.sh -t v1.0.0

# Build and push to Docker Hub
./docker-build.sh -t v1.0.0 --push

# Multi-platform build and push
./docker-build.sh -p linux/amd64,linux/arm64 --push

# Run local validation tests (mimics GitHub Action)
./docker-test.sh
```

### Prerequisites for Manual Push
```bash
# Login to Docker Hub
docker login

# Or use environment variables
export DOCKER_USERNAME=filipe958
export DOCKER_PASSWORD=your_access_token
./docker-build.sh --push
```

## 📦 Using the Docker Images

### Pull and Run
```bash
# Latest version
docker pull filipe958/pubsub-sub-bench:latest
docker run --rm filipe958/pubsub-sub-bench:latest --help

# Specific version
docker pull filipe958/pubsub-sub-bench:v1.2.3
docker run --rm filipe958/pubsub-sub-bench:v1.2.3 --help
```

### Example Usage
```bash
# Subscribe mode
docker run --rm filipe958/pubsub-sub-bench:latest \
  --host redis-server --port 6379 \
  --mode subscribe --clients 10

# Publish mode
docker run --rm filipe958/pubsub-sub-bench:latest \
  --host redis-server --port 6379 \
  --mode publish --clients 5 --test-time 60

# With Docker network
docker run --rm --network redis-net filipe958/pubsub-sub-bench:latest \
  --host redis-container --mode subscribe
```

## 🔍 Monitoring and Troubleshooting

### GitHub Actions
- Check workflow runs in **Actions** tab
- View build logs and summaries
- Monitor security scan results

### PR Validation
- **Automatic Docker build validation** on all pull requests
- Tests both single-platform (`linux/amd64`) and multi-platform builds
- Validates basic functionality (help, version commands)
- Provides PR comments with build status and details
- Prevents merging PRs with broken Docker builds

### Docker Hub
- View images at: https://hub.docker.com/r/filipe958/pubsub-sub-bench
- Check image sizes and platforms
- Review vulnerability scan results

### Common Issues

1. **Authentication Failed**
   - Verify `DOCKER_USERNAME` and `DOCKER_PASSWORD` secrets
   - Ensure access token has correct permissions

2. **Build Failed**
   - Check Go version compatibility
   - Verify Dockerfile syntax
   - Review build logs in Actions

3. **Push Failed**
   - Confirm repository exists on Docker Hub
   - Check network connectivity
   - Verify permissions

## 🏗️ Architecture

### Multi-Stage Build
- **Stage 1**: Go build environment with full toolchain
- **Stage 2**: Minimal Alpine runtime with only the binary
- **Result**: Optimized image size with security best practices

### Security Features
- Non-root user execution
- Minimal attack surface
- Vulnerability scanning with Trivy
- Signed images (when configured)

### Performance Optimizations
- Layer caching for faster builds
- Multi-platform support
- Efficient .dockerignore
- Build argument optimization
