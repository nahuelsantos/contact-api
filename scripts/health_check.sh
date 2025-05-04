#!/bin/bash

# Health check script for mail-api

# Default values
HOST="localhost"
PORT="20001"
ENDPOINT="/health"

# Parse command line options
while [[ $# -gt 0 ]]; do
  case $1 in
    -h|--host)
      HOST="$2"
      shift 2
      ;;
    -p|--port)
      PORT="$2"
      shift 2
      ;;
    -e|--endpoint)
      ENDPOINT="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

URL="http://${HOST}:${PORT}${ENDPOINT}"
echo "Checking health at: $URL"

# Make the request
response=$(curl -s -w "\n%{http_code}" "$URL")
status_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | sed '$d')

# Check if the request was successful
if [[ $status_code -eq 200 ]]; then
  echo "Health check succeeded with status code: $status_code"
  echo "Response: $body"
  
  # Check for success field in JSON response
  if echo "$body" | grep -q '"success":true'; then
    echo "Service is healthy!"
    exit 0
  else
    echo "Service returned 200 but may not be healthy. Check response."
    exit 1
  fi
else
  echo "Health check failed with status code: $status_code"
  echo "Response: $body"
  exit 1
fi 