#!/bin/bash

IMAGE_NAME="kind-with-podman:latest"
DOCKERFILE="kind-node-podman.Dockerfile"

if [ -z "$(docker images -q $IMAGE_NAME)" ]; then
    docker build -f "$DOCKERFILE" -t "$IMAGE_NAME" .
fi

echo "Building custom kind image with podman..."

if [ $? -eq 0 ]; then
    echo "Successfully built image: $IMAGE_NAME"
    echo "The image is now ready for use in clab kind clusters."
else
    echo "Failed to build image"
    exit 1
fi
