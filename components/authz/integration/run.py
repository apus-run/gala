#!/usr/bin/env python3
"""Run authz integration tests in a disposable Docker Compose project."""
import os
from pathlib import Path
import shutil
import subprocess
import sys
import uuid

root = Path(__file__).resolve().parent.parent
project = "gala-authz-" + uuid.uuid4().hex[:10]
compose = ["docker", "compose", "-p", project, "-f", str(root / "integration/compose.yaml")]


def run(args, **kwargs):
    subprocess.run(args, check=True, **kwargs)


def endpoint(service, port):
    return subprocess.check_output(compose + ["port", service, str(port)], text=True).strip()


status = 1
try:
    run(compose + ["up", "-d", "--wait", "--wait-timeout", "180"])
    env = os.environ.copy()
    env.pop("GOROOT", None)
    env["AUTHZ_REDIS_ADDR"] = endpoint("redis", 6379)
    env["AUTHZ_CLUSTER_ADDRS"] = ",".join(endpoint("cluster", p) for p in (7000, 7001, 7002))
    env["AUTHZ_MYSQL_DSN"] = "root:authz-integration-only@tcp(" + endpoint("mysql", 3306) + ")/authz_test?parseTime=true&timeout=5s&readTimeout=5s&writeTimeout=5s"
    host, port = endpoint("postgres", 5432).rsplit(":", 1)
    env["AUTHZ_POSTGRES_DSN"] = f"host={host} port={port} user=postgres password=authz-integration-only dbname=authz_test sslmode=disable connect_timeout=5"
    go = env.get("AUTHZ_GO_BINARY") or shutil.which("go")
    if not go:
        raise RuntimeError("Go not found; set AUTHZ_GO_BINARY")
    run([go, "version"], env=env, cwd=root)
    for service, binary in (("redis", ["redis-server", "--version"]), ("cluster", ["redis-server", "--version"]), ("mysql", ["mysqld", "--version"]), ("postgres", ["postgres", "--version"])):
        run(compose + ["exec", "-T", service] + binary)
    run([go, "test", "-tags=integration", "-race", "-count=1", "-timeout=180s", "-v", "-run", "^TestDocker", "."], env=env, cwd=root)
    status = 0
except (subprocess.CalledProcessError, RuntimeError) as exc:
    print(exc, file=sys.stderr)
    subprocess.run(compose + ["logs", "--tail", "60"])
finally:
    cleanup = subprocess.run(compose + ["down", "-v", "--remove-orphans"])
    if cleanup.returncode:
        status = cleanup.returncode
    print("Docker project:", project, "result:", "PASS" if status == 0 else "FAIL", flush=True)
sys.exit(status)
