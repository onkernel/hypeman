# Verification Steps for OpenAPI Exec Update

## Files Modified

1. `openapi.yaml` - Updated ExecRequest schema and /instances/{id}/exec endpoint
2. `lib/oapi/oapi.go` - Manually updated generated code to match new spec
3. `regenerate.sh` - Script to regenerate code from spec (created)
4. `OPENAPI_EXEC_UPDATE.md` - Documentation of changes (created)

## Verification Commands

Run these commands in order to verify the changes:

### 1. Regenerate OpenAPI Code (Recommended)

This will ensure the generated code exactly matches the spec:

```bash
cd /workspace/repo-76e8dc9d-020e-4ec1-93c2-ad0a593aa1a6
make oapi-generate
```

Or use the script:

```bash
chmod +x regenerate.sh
./regenerate.sh
```

### 2. Build the Project

```bash
make build
```

Expected output: Build should succeed without errors.

### 3. Run Tests

```bash
make test
```

Expected output: All tests should pass, including the exec tests.

### 4. Check Git Diff

```bash
git diff openapi.yaml
git diff lib/oapi/oapi.go
```

Review the changes to ensure they match the expected modifications.

## What Was Changed

### openapi.yaml

- **ExecRequest schema**: Added `env`, `cwd`, `timeout` fields; made `command` optional; changed `tty` default to `false`
- **/instances/{id}/exec endpoint**: Removed query parameters; added WebSocket protocol description

### lib/oapi/oapi.go

- **Added ExecRequest type**: New struct matching the schema
- **Updated ExecInstanceParams**: Removed query parameter fields (now empty)
- **Updated ServerInterfaceWrapper.ExecInstance**: Removed query parameter binding
- **Updated NewExecInstanceRequest**: Removed query parameter encoding

## Expected Behavior

After these changes:

1. The OpenAPI spec accurately documents the WebSocket-based exec protocol
2. The generated code no longer expects query parameters for exec
3. API clients can see the correct ExecRequest schema with all fields
4. The implementation in `cmd/api/api/exec.go` remains unchanged (it was already correct)

## Troubleshooting

If build fails:
- Ensure Go is installed and in PATH
- Run `go mod tidy` to update dependencies
- Check that `bin/oapi-codegen` exists or run `make install-tools`

If tests fail:
- Check that KVM is available (`/dev/kvm` exists)
- Ensure user is in `kvm` group
- Verify network capabilities are set correctly

## Manual Verification

To manually verify the exec functionality works:

1. Start the API server
2. Create an instance
3. Connect to the exec endpoint via WebSocket
4. Send an ExecRequest JSON message with fields: `{"command": ["/bin/sh"], "tty": false}`
5. Verify bidirectional communication works
6. Check that exit code is returned in final JSON message
