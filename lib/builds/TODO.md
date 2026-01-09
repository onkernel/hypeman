# Build System TODOs

Outstanding issues and improvements for the build system.

---

## ✅ Completed

### 1. Enable cgroups for BuildKit Secrets

**Status:** ✅ Implemented (Option A)

**Changes:** Added cgroup2 mount to `lib/system/init/mount.go`:
- `mountEssentials()` now mounts `/sys/fs/cgroup` with cgroup2 filesystem
- `bindMountsToNewRoot()` now bind-mounts cgroups to the new root

**To activate:** Rebuild the embedded binaries, then start the API server:
```bash
make build-embedded  # Rebuilds lib/system/init/init
make dev             # Or: make build && ./bin/hypeman
```
The initrd is automatically rebuilt on first VM start when it detects the embedded binaries have changed.

---

## 🟢 Low Priority

### 2. Builder Image Tooling

**File:** `lib/builds/images/README.md`

**Suggestion:** Create a script or tooling for building and publishing new builder images.
