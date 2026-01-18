#!/usr/bin/env python3
"""
CBox Callback Library

This library provides a simple interface for guest VM code to make RPC callbacks
to the host client through the vsock server.

Usage:
    from cbox_callback import callback

    # Make a callback with no parameters
    result = callback("get_current_time")

    # Make a callback with parameters
    result = callback("process_data", {"input": "hello", "count": 5})

    # The result is whatever the client handler returns (as a dict or primitive)
    print(result)
"""

import json
import socket
import sys
from typing import Any, Optional


# vsock server port (must match the vsockserver's port)
VSOCK_PORT = 4032

# vsock CID for host (always 2 in the vsock protocol)
VSOCK_HOST_CID = 2


def callback(method: str, params: Optional[dict] = None, timeout: float = 30.0) -> Any:
    """
    Make an RPC callback to the host client.

    Args:
        method: The callback method name to invoke on the client.
        params: Optional dictionary of parameters to pass to the callback.
        timeout: Timeout in seconds for the callback (default: 30s).

    Returns:
        The result from the client's callback handler.

    Raises:
        RuntimeError: If the callback fails or returns an error.
        TimeoutError: If the callback times out.
        ConnectionError: If unable to connect to the vsock server.
    """
    try:
        # Create vsock connection to the local vsockserver
        sock = socket.socket(socket.AF_VSOCK, socket.SOCK_STREAM)
        sock.settimeout(timeout)
        sock.connect((VSOCK_HOST_CID, VSOCK_PORT))
    except socket.error as e:
        raise ConnectionError(f"Failed to connect to vsock server: {e}")

    try:
        # Build the CALLBACK command
        if params is not None:
            params_json = json.dumps(params)
            command = f"CALLBACK {method} {params_json}\n"
        else:
            command = f"CALLBACK {method}\n"

        # Send the command
        sock.sendall(command.encode('utf-8'))

        # Read the response
        response = b""
        while True:
            chunk = sock.recv(4096)
            if not chunk:
                break
            response += chunk
            if response.endswith(b'\n'):
                break

        response_str = response.decode('utf-8').strip()

        # Check for error
        if response_str.startswith("Error:"):
            raise RuntimeError(response_str)

        # Parse the result
        try:
            return json.loads(response_str)
        except json.JSONDecodeError:
            # Return as string if not valid JSON
            return response_str

    except socket.timeout:
        raise TimeoutError(f"Callback '{method}' timed out after {timeout}s")
    finally:
        sock.close()


def callback_async(method: str, params: Optional[dict] = None) -> None:
    """
    Make a fire-and-forget callback to the host client.
    This does not wait for a response.

    Args:
        method: The callback method name to invoke on the client.
        params: Optional dictionary of parameters to pass to the callback.

    Note:
        Errors are silently ignored. Use callback() if you need error handling.
    """
    try:
        callback(method, params, timeout=5.0)
    except Exception:
        pass  # Fire and forget


# Command-line interface for shell scripts
if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python cbox_callback.py <method> [params_json]", file=sys.stderr)
        print("Example: python cbox_callback.py get_time", file=sys.stderr)
        print("Example: python cbox_callback.py process '{\"data\": \"hello\"}'", file=sys.stderr)
        sys.exit(1)

    method_name = sys.argv[1]
    params_dict = None

    if len(sys.argv) > 2:
        try:
            params_dict = json.loads(sys.argv[2])
        except json.JSONDecodeError as e:
            print(f"Error: Invalid JSON parameters: {e}", file=sys.stderr)
            sys.exit(1)

    try:
        result = callback(method_name, params_dict)
        print(json.dumps(result))
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)
