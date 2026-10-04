#!/bin/bash
set -e

VERSION=${1:-"latest"}
RELEASE_DIR="go-trade-bot-${VERSION}"

echo "Creating release artifact: ${RELEASE_DIR}.tar.gz..."

# 1. Clean up and create release dir
rm -rf ${RELEASE_DIR}
mkdir -p ${RELEASE_DIR}

# 2. Copy the docker configurations and required files
cp docker-compose.yml Dockerfile Makefile prometheus.yml config.example.yml ${RELEASE_DIR}/
cp config.example.yml ${RELEASE_DIR}/config.yml

# 3. Copy source files, ignoring non-essential development/compiled folders
# This ensures a lightweight deployment footprint.
rsync -avq --exclude='.git' \
          --exclude='node_modules' \
          --exclude='web/dist' \
          --exclude='web/node_modules' \
          --exclude='cmd/api/webui/dist' \
          --exclude='.DS_Store' \
          --exclude='.claude' \
          --exclude='.tmpbin' \
          --exclude='api' \
          --exclude='worker' \
          --exclude='mcp' \
          --exclude='agent' \
          --exclude='go-trade-bot-*' \
          ./ ${RELEASE_DIR}/

# 4. Create the final tarball
tar -czf ${RELEASE_DIR}.tar.gz ${RELEASE_DIR}

# 5. Clean up directory
rm -rf ${RELEASE_DIR}

echo "✅ Artifact successfully created: ${RELEASE_DIR}.tar.gz"
echo "To deploy, simply upload this tarball to your target server, extract it, and run:"
echo "  make up"
