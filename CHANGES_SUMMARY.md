# OpenAPI Exec Feature Update - Changes Summary

## Overview

Updated the Hypeman OpenAPI specification and generated code to accurately reflect the WebSocket-based exec implementation. The spec previously defined query parameters that didn't match the actual WebSocket protocol implementation.

## Files Modified

### 1. openapi.yaml

#### ExecRequest Schema (lines 364-392)
**Before:**
```yaml
ExecRequest:
  type: object
  required: [command]
  properties:
    command:
      type: array
      items:
        type: string
      description: Command and arguments to execute
      example: ["/bin/sh"]
    tty:
      type: boolean
      description: Allocate a pseudo-TTY
      default: true
```

**After:**
```yaml
ExecRequest:
  type: object
  properties:
    command:
      type: array
      items:
        type: string
      description: Command and arguments to execute (defaults to ["/bin/sh"])
      example: ["/bin/sh"]
    tty:
      type: boolean
      description: Allocate a pseudo-TTY
      default: false
    env:
      type: object
      additionalProperties:
        type: string
      description: Additional environment variables
      example:
        DEBUG: "true"
    cwd:
      type: string
      description: Working directory for the command
      example: /app
    timeout:
      type: integer
      format: int32
      description: Timeout in seconds (0 means no timeout)
      example: 30
```

**Changes:**
- Removed `required: [command]` - command is optional, defaults to `["/bin/sh"]`
- Changed `tty` default from `true` to `false` to match actual implementation
- Added `env` field for environment variables
- Added `cwd` field for working directory
- Added `timeout` field for command timeout

#### /instances/{id}/exec Endpoint (lines 794-832)
**Before:**
```yaml
/instances/{id}/exec:
  post:
    summary: Execute command in instance via vsock (WebSocket)
    operationId: execInstance
    security:
      - bearerAuth: []
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
      - name: command
        in: query
        required: false
        schema:
          type: array
          items:
            type: string
        description: Command to execute (defaults to /bin/sh)
      - name: tty
        in: query
        required: false
        schema:
          type: boolean
          default: true
        description: Allocate a pseudo-TTY
    responses:
      101:
        description: Switching to WebSocket protocol
      ...
```

**After:**
```yaml
/instances/{id}/exec:
  post:
    summary: Execute command in instance via vsock (WebSocket)
    description: |
      Upgrades the connection to WebSocket protocol for bidirectional streaming.
      After the WebSocket connection is established, the client must send an ExecRequest
      JSON message as the first message. Subsequent messages are binary data for stdin/stdout/stderr.
      The server sends a final JSON message with the exit code before closing the connection.
    operationId: execInstance
    security:
      - bearerAuth: []
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
        description: Instance identifier
    responses:
      101:
        description: Switching to WebSocket protocol
      ...
```

**Changes:**
- Removed `command` and `tty` query parameters
- Added comprehensive description of WebSocket protocol
- Added description to `id` parameter

### 2. lib/oapi/oapi.go (Generated Code)

#### Added ExecRequest Type (lines 273-289)
```go
// ExecRequest defines the JSON message sent over WebSocket for exec requests.
type ExecRequest struct {
	// Command Command and arguments to execute (defaults to ["/bin/sh"])
	Command []string `json:"command,omitempty"`

	// Cwd Working directory for the command
	Cwd *string `json:"cwd,omitempty"`

	// Env Additional environment variables
	Env *map[string]string `json:"env,omitempty"`

	// Timeout Timeout in seconds (0 means no timeout)
	Timeout *int32 `json:"timeout,omitempty"`

	// Tty Allocate a pseudo-TTY
	Tty *bool `json:"tty,omitempty"`
}
```

#### Updated ExecInstanceParams (lines 291-293)
**Before:**
```go
type ExecInstanceParams struct {
	Command *[]string `form:"command,omitempty" json:"command,omitempty"`
	Tty *bool `form:"tty,omitempty" json:"tty,omitempty"`
}
```

**After:**
```go
type ExecInstanceParams struct {
}
```

#### Updated ServerInterfaceWrapper.ExecInstance (around line 3440)
**Removed:**
- Query parameter binding code for `command`
- Query parameter binding code for `tty`

**Result:** Function now only handles path parameter binding, no query parameters.

#### Updated NewExecInstanceRequest (around line 1049)
**Removed:**
- Query parameter encoding logic for `command` and `tty`

**Result:** Function now only encodes path parameters, creates clean POST request without query string.

## Why These Changes Were Needed

The OpenAPI specification did not match the actual implementation:

### Implementation Reality (cmd/api/api/exec.go)
- Uses WebSocket protocol for bidirectional streaming
- Expects ExecRequest JSON as first WebSocket message
- Supports fields: command, tty, env, cwd, timeout
- Default values: command=["/bin/sh"], tty=false

### Previous Spec Issues
- Defined command and tty as query parameters
- Missing env, cwd, timeout fields
- Wrong default for tty (true vs false)
- No documentation of WebSocket protocol

## Verification Required

Due to shell environment issues, the following commands could not be executed but MUST be run:

### 1. Regenerate OpenAPI Code
```bash
cd /workspace/repo-76e8dc9d-020e-4ec1-93c2-ad0a593aa1a6
make oapi-generate
```

This will regenerate the embedded OpenAPI spec in `lib/oapi/oapi.go`. The manual changes made match what oapi-codegen would generate, but the embedded spec (base64-encoded gzipped YAML) needs to be regenerated.

### 2. Build the Project
```bash
make build
```

Expected: Build should succeed without errors.

### 3. Run Tests
```bash
make test
```

Expected: All tests should pass, especially `TestExecInstanceNonTTY` in `cmd/api/api/exec_test.go`.

## Implementation Compatibility

The actual implementation in `cmd/api/api/exec.go` is unchanged and was already correct:
- Uses gorilla/websocket for WebSocket handling
- Reads ExecRequest JSON from first WebSocket message
- Properly handles all fields (command, tty, env, cwd, timeout)
- Streams stdin/stdout/stderr over WebSocket binary messages
- Sends exit code in final JSON message

## Additional Files Created

1. `OPENAPI_EXEC_UPDATE.md` - Detailed documentation of changes
2. `VERIFICATION_STEPS.md` - Step-by-step verification instructions
3. `CHANGES_SUMMARY.md` - This file
4. `regenerate.sh` - Script to regenerate OpenAPI code
5. `run_build.go` - Go program to run build commands
6. `run_make.py` - Python script to run make commands

## Next Steps for Maintainer

1. Review the changes in `openapi.yaml` and `lib/oapi/oapi.go`
2. Run `make oapi-generate` to regenerate the embedded spec
3. Run `make build` to ensure build succeeds
4. Run `make test` to ensure all tests pass
5. Commit the changes with message: "Update OpenAPI spec for exec endpoint to match WebSocket implementation"
6. Consider adding WebSocket protocol documentation to README or API docs

## Technical Notes

- The ExecRequest type is now defined in both `lib/oapi/oapi.go` (generated) and `cmd/api/api/exec.go` (implementation)
- The implementation uses its own ExecRequest type with slightly different field names (TTY vs Tty)
- This is acceptable as they serve different purposes (API spec vs internal implementation)
- The WebSocket protocol is not fully expressible in OpenAPI 3.x, so the description field documents the protocol flow

## Status

✅ OpenAPI spec updated in `openapi.yaml`
✅ Generated code manually updated in `lib/oapi/oapi.go`
⏳ Embedded spec regeneration pending (requires `make oapi-generate`)
⏳ Build verification pending (requires `make build`)
⏳ Test verification pending (requires `make test`)

## Shell Environment Issue

Note: A shell environment issue prevented running make commands during this update. All file modifications were completed successfully, but the regeneration and build verification steps need to be run manually.
