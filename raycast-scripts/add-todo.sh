#!/bin/bash

# Required parameters:
# @raycast.schemaVersion 1
# @raycast.title Add Daytracker Todo
# @raycast.mode silent

# Optional parameters:
# @raycast.icon 📝
# @raycast.argument1 { "type": "text", "placeholder": "Task title" }
# @raycast.packageName Daytracker

TITLE="$1"
TODAY=$(date +%Y-%m-%d)
PORT="${DAYTRACKER_PORT:-8080}"

response=$(curl -sf -X POST "http://localhost:${PORT}/api/days/${TODAY}/tasks" \
  -H "Content-Type: application/json" \
  -d "{\"title\": $(echo "$TITLE" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read().strip()))')}")

if [ $? -ne 0 ]; then
  echo "Failed to add task — is daytracker running on port ${PORT}?"
  exit 1
fi

echo "Added: $TITLE"
