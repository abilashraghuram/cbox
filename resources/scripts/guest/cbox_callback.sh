#!/bin/bash
#
# CBox Callback Script
#
# This script provides a simple interface for making RPC callbacks to the host
# client from within a guest VM. It communicates with the local vsockserver.
#
# Usage:
#   cbox_callback <method> [params_json]
#
# Examples:
#   cbox_callback get_current_time
#   cbox_callback process_data '{"input": "hello", "count": 5}'
#

set -e

METHOD="$1"
PARAMS="$2"

if [ -z "$METHOD" ]; then
    echo "Usage: $0 <method> [params_json]" >&2
    echo "Example: $0 get_time" >&2
    echo "Example: $0 process '{\"data\": \"hello\"}'" >&2
    exit 1
fi

# Build the callback command
if [ -n "$PARAMS" ]; then
    COMMAND="CALLBACK $METHOD $PARAMS"
else
    COMMAND="CALLBACK $METHOD"
fi

# Use socat to connect to the vsock server
# CID 2 is the host, port 4032 is the vsockserver port
RESPONSE=$(echo "$COMMAND" | socat - VSOCK-CONNECT:2:4032 2>/dev/null)

# Check for error
if echo "$RESPONSE" | grep -q "^Error:"; then
    echo "$RESPONSE" >&2
    exit 1
fi

# Output the result
echo "$RESPONSE"
