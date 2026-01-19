# CBox - Lightweight MicroVM Execution Environment

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Host System                              │
│  ┌─────────────────┐    ┌─────────────────────────────────────┐ │
│  │ cbox-restserver │◄───│  External Client (with callback URL) │ │
│  │    (port 7000)  │    └─────────────────────────────────────┘ │
│  └────────┬────────┘                                            │
│           │                                                      │
│           ▼                                                      │
│  ┌─────────────────┐                                            │
│  │ Cloud Hypervisor│                                            │
│  │      (VMM)      │                                            │
│  └────────┬────────┘                                            │
│           │                                                      │
├───────────┼──────────────────────────────────────────────────────┤
│           │              Guest VM                                │
│           ▼                                                      │
│  ┌─────────────────┐    ┌─────────────────┐                     │
│  │ cbox-cmdserver  │    │ cbox-vsockserver│                     │
│  │   (port 4031)   │    │   (port 4032)   │                     │
│  └─────────────────┘    └─────────────────┘                     │
└─────────────────────────────────────────────────────────────────┘
```

## MCP Callback Flow

How Python code running inside the VM invokes external MCP servers on the host:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Host System                                     │
│                                                                              │
│  ┌──────────────┐      ┌─────────────────┐      ┌────────────────────────┐  │
│  │ MCP Servers  │◄─────│ External Client │◄─────│    cbox-restserver     │  │
│  │ (tools/APIs) │ (5)  │ (callback handler)│ (4) │     (port 7000)        │  │
│  └──────┬───────┘      └─────────────────┘      └───────────▲────────────┘  │
│         │                                                    │               │
│         │ (6) Tool Result                                   │ (3) HTTP POST │
│         ▼                                                    │  /v1/internal │
│  ┌──────────────┐      ┌─────────────────┐                  │  /callback    │
│  │ MCP Servers  │─────►│ External Client │─────────────────►│               │
│  └──────────────┘ (7)  └─────────────────┘       (8)        │               │
│                                                              │               │
├──────────────────────────────────────────────────────────────┼───────────────┤
│                          Guest VM                            │               │
│                                                              │               │
│  ┌──────────────────────────────────────┐    ┌──────────────┴────────────┐  │
│  │         Python Code                   │    │    cbox-vsockserver       │  │
│  │  ┌────────────────────────────────┐  │    │       (port 4032)          │  │
│  │  │ from cbox_callback import      │  │    │                            │  │
│  │  │      callback                  │  │    │  Forwards callback to      │  │
│  │  │                                │  │    │  restserver via virtio-net │  │
│  │  │ result = callback(             │  │    │                            │  │
│  │  │   "mcp_tool_name",       ──────┼──┼───►│                            │  │
│  │  │   {"param": "value"}     (1)   │  │(2) │                            │  │
│  │  │ )                              │  │    │                            │  │
│  │  │                                │  │    │                            │  │
│  │  │ # result contains MCP    ◄─────┼──┼────│                            │  │
│  │  │ # server response        (9)   │  │    │                            │  │
│  │  └────────────────────────────────┘  │    └───────────────────────────┘  │
│  └──────────────────────────────────────┘                                    │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘

Flow:
  (1) Python calls callback("mcp_tool_name", params)
  (2) cbox_callback connects to vsockserver via vsock
  (3) vsockserver forwards request to restserver over virtio-net
  (4) restserver sends HTTP POST to external client's callback URL
  (5) External client invokes the appropriate MCP server/tool
  (6) MCP server returns result to external client
  (7) External client formats response
  (8) Response sent back to restserver
  (9) Result propagates back to Python code in VM
```
