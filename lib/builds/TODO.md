# Build System TODOs

Outstanding issues and improvements for the build system.

---

## ✅ Completed

### 1. Enable cgroups for BuildKit Secrets

**Status:** ✅ Implemented and tested

**Changes:** Added cgroup2 mount to `lib/system/init/mount.go`:
- `mountEssentials()` now mounts `/sys/fs/cgroup` with cgroup2 filesystem
- `bindMountsToNewRoot()` now bind-mounts cgroups to the new root

**Verified:** E2E build test passes (`./scripts/e2e-build-test.sh`)

---

## 🟢 Low Priority

### 2. Builder Image Tooling

**File:** `lib/builds/images/README.md`

**Suggestion:** Create a script or tooling for building and publishing new builder images.
