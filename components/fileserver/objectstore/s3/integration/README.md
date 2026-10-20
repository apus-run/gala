# RustFS real-server verification

The harness uses RustFS 1.0.3, resolves and records its immutable image ID, and creates unique containers, credentials, volumes and buckets. Only loopback ports are published. It removes only this run's resources, including disposable test data, when finished. No existing bucket or container is reused.

Run from the repository root with Docker Engine available:

```sh
python3 components/fileserver/objectstore/s3/integration/run.py
python3 components/fileserver/objectstore/s3/integration/cluster_run.py
```

The first runner covers the shared Storage contract, multipart boundaries, conditional races, version consistency, checksum validation, abort cleanup, presigning, authentication, lost acknowledgments, process SIGKILL and container replacement. Two single-factor mutations must fail against the live service: treating SDK status zero as a response and restoring SDK automatic commit retries.

The second starts four nodes with four logical drive directories each (one Docker volume per node). It checks acknowledged objects after losing one node, writes through the surviving cluster, rejects writes after losing two nodes, then reads again after recovery. RustFS rejects colocated drive paths by default; this disposable test explicitly sets `RUSTFS_UNSAFE_BYPASS_DISK_CHECK=true`. All volumes are on one Docker VM; this validates process failure and quorum logic, not independent physical disks or sites.

Raw evidence is written to `audit/rustfs/`; the report there states the actual result and limitations. `RUSTFS_IMAGE` can override the default image for another run. The single-node runner pulls it; pull explicitly before using the cluster runner alone.

The Go integration tests skip by default. To target an independently provisioned test server, explicitly set `RUSTFS_TEST_ENDPOINT`, `RUSTFS_ACCESS_KEY`, `RUSTFS_SECRET_KEY`, and a dedicated `RUSTFS_TEST_BUCKET`, then run `go test -race -count=1 -run '^TestRustFS' .` in the S3 module. This direct mode creates test objects and does not automatically remove your bucket. Never point it at a production bucket.

Compose files are test fixtures, not production deployment templates. Production needs its own physical failure domains, TLS, restricted credentials, capacity and recovery acceptance criteria.

For the hardened Docker profile, TLS, tenant IAM and destructive isolated source-volume recovery, run `python3 components/fileserver/objectstore/s3/integration/acceptance_run.py` from the repo root. This runner uses `../deploy/compose.yaml` directly and records evidence in `audit/rustfs/acceptance/`.
