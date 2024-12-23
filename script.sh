set -e

# Start dockerd in the background
./main &

sleep 5

python3 handler.py