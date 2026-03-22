#!/bin/bash
set -e

# Initialize data directory from defaults if first run
if [ ! -f /agent/data/prompt.md ]; then
    echo "First run: initializing agent data from defaults..."
    cp -r /agent/defaults/* /agent/data/
fi

# Run brain with auto-restart and binary swap
while true; do
    echo "Starting brain server..."
    /agent/brain
    EXIT_CODE=$?
    echo "Brain exited with code $EXIT_CODE"

    # If a new binary was built, swap it in
    if [ -f /agent/brain.new ]; then
        echo "New binary found, swapping..."
        mv /agent/brain.new /agent/brain
        chmod +x /agent/brain
    fi

    echo "Restarting in 1s..."
    sleep 1
done
