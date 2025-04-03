<p align="center">
  <img src="https://res.cloudinary.com/diroilukd/image/upload/v1743710311/ChatGPT_Image_Apr_4_2025_01_27_52_AM_zi2sjz.png" alt="Falcon VOD Logo" width="200">
</p>

# Falcon VOD

**⚠️ WORK IN PROGRESS ⚠️**

## Overview

Falcon is an open-source video platform that provides end-to-end video processing, storage, and streaming capabilities. The platform consists of multiple services that handle uploading, transcoding, and streaming video content.

## Architecture

The platform consists of several key services:

- **Uploader Service**: Handles video file uploads
- **Transcoder Service**: Processes videos into various formats (HLS, DASH)
- **Streamer Service**: Manages video streaming to clients
- **Main API**: Central coordination and management API

## Getting Started

### Prerequisites

- Go 1.19+
- FFmpeg
- Docker and Docker Compose

### Environment Setup

1. Clone the repository:
   ```
   git clone https://github.com/suyash-thakur/falcon.git
   cd falcon
   ```

2. Copy the example environment files:
   ```
   cp .env.example .env
   cp frontend/api/.env.example frontend/api/.env
   ```
   
   Edit the `.env` files with your specific configuration values.

3. Copy the example configuration file:
   ```
   cp backend/config.example.yaml backend/config.yaml
   ```

4. Run the services using Docker Compose:
   ```
   docker-compose -f docker/docker-compose.yaml up
   ```

## Development

### Complete Directory Structure

```
falcon/
├── .git/             # Git repository data
├── .gitignore        # Git ignore file
├── README.md         # Project documentation
├── tech-doc.md       # Technical documentation
├── .env.example      # Example root environment variables
├── backend/          # Backend services
│   ├── api/          # API definitions and handlers
│   ├── cmd/          # Command entrypoints for services
│   │   ├── uploader/    # Uploader service
│   │   ├── transcoder/  # Transcoder service
│   │   └── streamer/    # Streamer service
│   ├── internal/     # Internal packages
│   ├── config.yaml   # Backend configuration
│   ├── config.example.yaml  # Example backend configuration
│   ├── go.mod        # Go module definition
│   └── go.sum        # Go module checksums
├── docker/           # Docker configuration
│   ├── docker-compose.yaml     # Docker Compose configuration
│   ├── Dockerfile.backend      # Backend service Dockerfile
│   └── Dockerfile.frontend     # Frontend service Dockerfile
├── frontend/         # Frontend application
│   ├── api/          # Frontend API
│   │   ├── .env      # Frontend API environment variables
│   │   └── .env.example  # Example frontend API environment variables
│   ├── public/       # Static assets
│   ├── src/          # Source code
│   └── package.json  # Dependencies
└── scripts/          # Utility scripts
```

### Configuration

The project uses several configuration files:

1. **backend/config.yaml**: Manages backend service configuration including:
   - Server settings
   - Database connection
   - Redis cache
   - Storage configuration (S3/MinIO)
   - Temporal workflow
   - FFmpeg transcoding options

2. **.env**: Root environment variables for Docker and services
   
3. **frontend/api/.env**: Environment variables for the frontend API service

4. **docker/docker-compose.yaml**: Configures all services including:
   - PostgreSQL database
   - Redis cache
   - MinIO storage
   - Temporal workflow engine

## License

[MIT License](LICENSE)
