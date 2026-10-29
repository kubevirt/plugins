# Releasing

The SDK is versioned after KubeVirt: SDK `vX.Y.Z` targets KubeVirt `vX.Y` (see
[Compatibility](README.md#compatibility)).

## Branches

- `main` follows the plugin API of KubeVirt's development branch.
- `release-X.Y` holds SDK `vX.Y`. Cut it from `main` no later than when the
  first change for the KubeVirt `vX.(Y+1)` plugin API is merged to `main`,
  normally when KubeVirt branches its own `release-X.Y`. Tag `vX.Y.0` on it once
  KubeVirt `vX.Y.0` is released. When cutting it, set the `KUBEVIRT_VERSION`
  default in `hack/config.sh` on the branch to KubeVirt's newest
  `vX.Y.0-rc.N`.
- After the cut, tag `vX.(Y+1).0-alpha.0` on the first new `main` commit (not
  on the branch point, which `release-X.Y` shares), so pseudo-versions of
  later `main` commits sort above the `vX.Y.Z` releases.

## Support

`main` and the latest SDK minor are maintained. `release-X.Y` reaches end of
life when `vX.(Y+1).0` is tagged, and its periodic CI job is removed.

## KubeVirt version used by functional tests

`KUBEVIRT_VERSION` (default in `hack/config.sh`) selects the KubeVirt that
`make cluster-sync` deploys:

| Value | Deploys |
|-------|---------|
| `vX.Y.Z`, `vX.Y.Z-rc.N` | that exact release |
| `vX.Y` | the latest `vX.Y.Z` release, pre-releases excluded |
| `nightly` | the latest nightly build of KubeVirt `main` |

Defaults: on `release-X.Y`, an exact tag (the newest `vX.Y.0-rc.N` until
KubeVirt `vX.Y.0` is out, then the newest `vX.Y.Z`). On `main`, the newest
KubeVirt tag that carries the plugin API `main` implements, or `nightly` if
there is none yet. Periodic CI runs `main` against `nightly`, and `release-X.Y`
against `vX.Y` once `vX.Y.0` is out (before that, against the branch default).

## New minor release (vX.Y.0)

On `release-X.Y` (or on `main` if the branch is not cut yet), once KubeVirt
`vX.Y.0` is released:

1. Align dependencies with KubeVirt:
   - `kubevirt.io/api` to the latest `vX.Y.Z`.
   - `libvirt.org/go/libvirtxml`, `google.golang.org/grpc` and
     `google.golang.org/protobuf` to the versions in KubeVirt's `go.mod` at
     that tag.
   - `k8s.io/*`, including `k8s.io/client-go`, to the Kubernetes release
     `kubevirt.io/api` requires, then `go mod tidy`.
   - The Go images in `hack/builder/Dockerfile` (tag and digest) and
     `Dockerfile.test-plugin` to the Go minor KubeVirt builds with
     (`GIMME_GO_VERSION` in KubeVirt's `hack/builder/Dockerfile`).
2. Sync the hook API: check out the tag in `../kubevirt`, run
   `hack/update-proto.sh` from the repository root, and commit any change.
3. Set the `KUBEVIRT_VERSION` default in `hack/config.sh` to that tag.
4. Point `cluster/kubevirtci_tag.txt` and `KUBEVIRT_PROVIDER`
   (`cluster/kubevirtci.sh`) at a kubevirtci provider that KubeVirt `vX.Y`
   supports.
5. Update the compatibility table in `README.md`.
6. Validate:

   ```bash
   make test
   hack/dockerized go vet ./...  # all packages, including tests/
   hack/dockerized go run golang.org/x/vuln/cmd/govulncheck@latest ./...
   make cluster-up
   KUBEVIRT_VERSION=vX.Y make cluster-sync functest
   make cluster-down
   ```

7. If `release-X.Y` does not exist yet, create it from the validated commit.
   Tag the release on `release-X.Y` and publish GitHub release notes:

   ```bash
   git tag -a vX.Y.0 -m "SDK vX.Y.0 for KubeVirt vX.Y"
   git push upstream vX.Y.0
   ```

   Go module tags are immutable once the Go module proxy has fetched them.
   Never move or re-create a tag; release a new patch instead.

## Patch release (vX.Y.Z)

1. Cherry-pick fixes from `main` to `release-X.Y`, or fix the branch directly
   when `main` has moved on.
2. On every KubeVirt `vX.Y.Z`, repeat steps 3 and 6 above. When that release
   changes the hook API or the content of a dependency from step 1, repeat
   steps 1-3 and 5-6 instead.
3. Tag `vX.Y.(Z+1)` on `release-X.Y`.
