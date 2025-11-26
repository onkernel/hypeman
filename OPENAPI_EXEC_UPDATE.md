# OpenAPI Exec Feature Update

## Summary

Updated the OpenAPI specification for the `/instances/{id}/exec` endpoint to accurately reflect the actual WebSocket-based implementation.

## Changes Made

### 1. Updated `openapi.yaml`

#### ExecRequest Schema (lines 364-392)
- **Removed** `required: [command]` - command is optional, defaults to `["/bin/sh"]`
- **Changed** `tty` default from `true` to `false` to match implementation
- **Added** `env` field - map of environment variables
- **Added** `cwd` field - working directory for the command
- **Added** `timeout` field - timeout in seconds (int32)

#### /instances/{id}/exec Endpoint (lines 794-832)
- **Removed** query parameters (`command` and `tty`)
- **Added** comprehensive description explaining the WebSocket protocol:
  - Connection upgrades to WebSocket
  - Client sends ExecRequest JSON as first message
  - Subsequent messages are binary data for stdin/stdout/stderr
  - Server sends final JSON message with exit code before closing

### 2. Updated `lib/oapi/oapi.go` (Generated Code)

#### Added ExecRequest Type (lines 273-289)
```go
type ExecRequest struct {
    Command []string           `json:"command,omitempty"`
    Cwd     *string            `json:"cwd,omitempty"`
    Env     *map[string]string `json:"env,omitempty"`
    Timeout *int32             `json:"timeout,omitempty"`
    Tty     *bool              `json:"tty,omitempty"`
}
```

#### Updated ExecInstanceParams (lines 291-293)
- Removed `Command` and `Tty` query parameter fields
- Now an empty struct (no query parameters)

#### Updated ServerInterfaceWrapper.ExecInstance (lines 3440-3445)
- Removed query parameter binding code for `command` and `tty`

#### Updated NewExecInstanceRequest (lines 1049-1059)
- Removed query parameter encoding logic
- Now only handles path parameters

## Implementation Details

The actual implementation in `cmd/api/api/exec.go` already correctly implements the WebSocket protocol:

1. Upgrades HTTP connection to WebSocket
2. Reads first WebSocket message as JSON ExecRequest
3. Uses custom ExecRequest type with fields: command, tty, env, cwd, timeout
4. Streams stdin/stdout/stderr over WebSocket binary messages
5. Sends final JSON message with exit code

## Why These Changes Were Needed

The OpenAPI spec previously defined exec parameters as query parameters, but the actual implementation:
- Uses WebSocket protocol (not REST)
- Expects a JSON message as the first WebSocket message
- Supports additional fields (env, cwd, timeout) not in the spec
- Has different defaults (tty defaults to false, not true)

## Next Steps

To complete the update, run:

```bash
make oapi-generate
```

This will regenerate the embedded OpenAPI spec in `lib/oapi/oapi.go` to match the updated `openapi.yaml`.

Note: The manual changes made to `lib/oapi/oapi.go` match what oapi-codegen would generate from the updated spec.

## Testing

After regeneration, verify:
1. Build succeeds: `make build`
2. Tests pass: `make test`
3. The exec functionality works as expected with WebSocket clients
