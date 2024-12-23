set -e

# Start dockerd in the background
./main &

sleep 5

RUNPOD_ENDPOINT_BASE_URL=http://localhost:19981/v2 python3 handler.py