# Build System Roadmap

## Current State (v0.1)

- ✅ Source-to-image builds in isolated microVMs
- ✅ BuildKit-based builds with daemonless execution
- ✅ Tenant-isolated registry caching
- ✅ Node.js 20 and Python 3.12 runtimes
- ✅ Vsock communication for build results
- ✅ Cgroup mounting for container runtime support

## Planned Improvements

### Phase 1: Cache Optimization

**Goal**: Reduce build times by sharing common base layers across tenants.

#### Multi-tier Cache Strategy

```
Import order (first match wins):
1. shared/{runtime}/base      ← Pre-warmed with OS + runtime layers (read-only)
2. {tenant}/{runtime}/{hash}  ← Tenant-specific dependency layers

Export to:
→ {tenant}/{runtime}/{hash}   ← Only tenant-specific layers
```

#### Benefits
- **Fast builds**: Common layers (apt packages, Node.js binary, etc.) are shared
- **Tenant isolation**: Application dependencies remain isolated
- **No cross-tenant poisoning**: Tenants can only write to their own scope
- **Controlled shared cache**: Only operators can update the shared base cache

#### Implementation Tasks
- [ ] Update `cache.go` with `ImportCacheArgs() []string` returning multiple args
- [ ] Update `builder_agent/main.go` to handle multiple `--import-cache` flags
- [ ] Add CLI/API endpoint for pre-warming shared cache
- [ ] Create cron job or webhook to refresh shared cache on base image updates
- [ ] Document cache warming process in README

### Phase 2: Security Hardening

#### Secret Management
- [ ] Implement vsock-based secret injection (secrets never written to disk)
- [ ] Add secret scoping per build (which secrets a build can access)
- [ ] Audit logging for secret access during builds
- [ ] Integration with external secret managers (Vault, AWS Secrets Manager)

#### Network Policy
- [ ] Implement domain allowlist for `egress` mode
- [ ] Add `isolated` mode (no network access during build phase)
- [ ] Rate limiting on registry pushes to prevent abuse
- [ ] DNS filtering for allowed domains

#### Build Provenance & Supply Chain Security
- [ ] Sign build provenance with Sigstore/cosign
- [ ] SLSA Level 2 compliance (authenticated build process)
- [ ] SBOM (Software Bill of Materials) generation during builds
- [ ] Vulnerability scanning of built images before push

### Phase 3: Additional Runtimes

| Runtime | Package Managers | Priority |
|---------|-----------------|----------|
| Go 1.22+ | go mod | High |
| Ruby 3.3+ | bundler, gem | Medium |
| Rust | cargo | Medium |
| Java 21+ | Maven, Gradle | Medium |
| PHP 8.3+ | composer | Low |
| Custom Dockerfile | N/A | High |

#### Custom Dockerfile Support
- [ ] Allow users to provide their own Dockerfile
- [ ] Security review: sandbox custom Dockerfiles more strictly
- [ ] Validate Dockerfile doesn't use dangerous instructions
- [ ] Consider read-only base image allowlist

### Phase 4: Performance & Observability

#### Metrics (Prometheus)
- [ ] `hypeman_build_duration_seconds` - histogram by runtime, status
- [ ] `hypeman_build_cache_hits_total` - counter for cache hits/misses
- [ ] `hypeman_build_queue_wait_seconds` - time spent in queue
- [ ] `hypeman_build_vm_boot_seconds` - microVM boot time
- [ ] `hypeman_build_push_duration_seconds` - registry push time

#### Logging Improvements
- [ ] Structured JSON logs from builder agent
- [ ] Log streaming during build (not just after completion)
- [ ] Build log retention policy

#### Distributed Builds
- [ ] Build worker pool across multiple hosts
- [ ] Load balancing for build queue (consistent hashing by tenant?)
- [ ] Horizontal scaling of build capacity
- [ ] Worker health checks and automatic failover

## Security Model

### Threat Model

| Threat | Mitigation | Status |
|--------|------------|--------|
| Container escape to host | MicroVM isolation (separate kernel) | ✅ Implemented |
| Cross-tenant cache poisoning | Tenant-scoped cache paths | ✅ Implemented |
| Host kernel exploit | Separate kernel per VM | ✅ Implemented |
| Malicious dependency exfiltration | Network isolation (egress control) | 🔄 Partial |
| Secret theft during build | Vsock-only secret injection | 📋 Planned |
| Registry credential theft | Per-build short-lived tokens | 📋 Planned |
| Resource exhaustion (DoS) | VM resource limits | ✅ Implemented |
| Build log information leak | Tenant-scoped log access | ✅ Implemented |

### Security Boundaries

```
┌─────────────────────────────────────────────────────────────┐
│                        Host System                           │
│  ┌─────────────────────────────────────────────────────────┐│
│  │                   Hypeman API                            ││
│  │  - JWT authentication                                    ││
│  │  - Tenant isolation at API level                        ││
│  └─────────────────────────────────────────────────────────┘│
│                              │                               │
│  ┌───────────────────────────┼───────────────────────────┐  │
│  │         MicroVM Boundary (Cloud Hypervisor)            │  │
│  │  ┌─────────────────────────────────────────────────┐   │  │
│  │  │              Builder VM                          │   │  │
│  │  │  - Separate kernel                              │   │  │
│  │  │  - Ephemeral (destroyed after build)            │   │  │
│  │  │  - Limited network (egress only to registry)    │   │  │
│  │  │  - No access to other tenants' data             │   │  │
│  │  │  ┌─────────────────────────────────────────┐    │   │  │
│  │  │  │         BuildKit (rootless)              │    │   │  │
│  │  │  │  - User namespace isolation              │    │   │  │
│  │  │  │  - No real root privileges               │    │   │  │
│  │  │  └─────────────────────────────────────────┘    │   │  │
│  │  └─────────────────────────────────────────────────┘   │  │
│  └────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### Not Protected (By Design)

These are inherent to the build process and cannot be fully mitigated:

1. **Malicious code execution during package install** - `npm install` and `pip install` execute arbitrary code by design
2. **Supply chain attacks on upstream packages** - Typosquatting, compromised maintainers, etc.
3. **Tenant poisoning their own cache** - A tenant can push malicious layers to their own cache scope
4. **Information leakage via build output** - Malicious deps can encode secrets in build artifacts

## Open Questions

1. **Custom Dockerfiles**: Should we support user-provided Dockerfiles?
   - Pro: Flexibility for advanced users
   - Con: Larger attack surface, harder to secure
   - Possible middle ground: Allowlist of base images

2. **Cache TTL Policy**: How long should tenant caches be retained?
   - Options: 7 days, 30 days, size-based eviction, never (until explicit delete)
   - Consider: Storage costs vs build speed

3. **Build Artifact Signing**: Required for all builds or opt-in?
   - Required: Better security posture, SLSA compliance
   - Opt-in: Less friction for getting started

4. **Multi-arch Builds**: Worth the complexity?
   - Use case: Deploy same image to ARM and x86
   - Complexity: Requires QEMU or cross-compilation support

5. **Build Concurrency Limits**: Per-tenant or global?
   - Per-tenant: Fair sharing, prevents noisy neighbor
   - Global: Simpler, but one tenant could starve others

## References

- [BuildKit GitHub](https://github.com/moby/buildkit)
- [Rootless Containers](https://rootlesscontaine.rs/)
- [SLSA Framework](https://slsa.dev/)
- [Sigstore](https://www.sigstore.dev/)
- [Cloud Hypervisor](https://github.com/cloud-hypervisor/cloud-hypervisor)

