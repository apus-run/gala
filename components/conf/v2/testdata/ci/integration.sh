#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
run="gala-conf-$(date +%s)-$$"
peers="$run-peers"
clients="$run-clients"
reports=$(mktemp -d)
cleanup() {
  docker rm -fv "$run-1" "$run-2" "$run-3" "$run-consul" >/dev/null 2>&1 || true
  docker network rm "$peers" "$clients" >/dev/null 2>&1 || true
  rm -rf "$reports"
}
trap cleanup EXIT
# Reserve an available Docker subnet, then declare it explicitly so the
# partition test can reconnect n1 on its original advertised peer address.
docker network create "$peers" >/dev/null
subnet=$(docker network inspect -f '{{(index .IPAM.Config 0).Subnet}}' "$peers")
docker network rm "$peers" >/dev/null
docker network create --subnet "$subnet" "$peers" >/dev/null
docker network create "$clients" >/dev/null
peer_ip() { python3 -c 'import ipaddress,sys; print(ipaddress.ip_network(sys.argv[1])[10+int(sys.argv[2])])' "$subnet" "$1"; }
cluster="n1=http://$(peer_ip 1):2380,n2=http://$(peer_ip 2):2380,n3=http://$(peer_ip 3):2380"
for i in 1 2 3; do
  network=(--network "$peers" --ip "$(peer_ip "$i")")
  if [[ "$i" == 1 ]]; then network=(--network "$clients"); fi
  docker create --tmpfs /etcd-data:rw,size=256m --name "$run-$i" "${network[@]}" --network-alias "n$i" \
    -p 127.0.0.1::2379 quay.io/coreos/etcd:v3.5.15 \
    /usr/local/bin/etcd --name "n$i" --data-dir /etcd-data \
    --listen-client-urls http://0.0.0.0:2379 --advertise-client-urls "http://n$i:2379" \
    --listen-peer-urls http://0.0.0.0:2380 --initial-advertise-peer-urls "http://$(peer_ip "$i"):2380" \
    --initial-cluster "$cluster" --initial-cluster-token "$run" >/dev/null
  if [[ "$i" == 1 ]]; then docker network connect --ip "$(peer_ip 1)" --alias n1 "$peers" "$run-1"; fi
  docker start "$run-$i" >/dev/null
done
export CONF_TEST_CONSUL_TOKEN
CONF_TEST_CONSUL_TOKEN=$(python3 -c 'import uuid; print(uuid.uuid4())')
CONSUL_LOCAL_CONFIG="{\"acl\":{\"enabled\":true,\"default_policy\":\"deny\",\"tokens\":{\"initial_management\":\"$CONF_TEST_CONSUL_TOKEN\"}}}" \
docker run -d --tmpfs /consul/data:rw,size=64m --name "$run-consul" -p 127.0.0.1::8500 \
  -e CONSUL_LOCAL_CONFIG hashicorp/consul:1.19 agent -dev -node gala-integration -client 0.0.0.0 >/dev/null
published_port() {
  local endpoint
  for _ in {1..30}; do
    if endpoint=$(docker port "$1" "$2" 2>/dev/null) && [[ -n "$endpoint" ]]; then echo "$endpoint"; return; fi
    sleep 1
  done
  echo "port unavailable for $1" >&2; return 1
}
export CONF_TEST_ETCD CONF_TEST_ETCD_MAJORITY CONF_TEST_ETCD_PEER_IP CONF_TEST_CONSUL
export CONF_TEST_ETCD_NODE="$run-1" CONF_TEST_ETCD_PEERS="$peers"
CONF_TEST_ETCD=$(published_port "$run-1" 2379/tcp)
CONF_TEST_ETCD_MAJORITY=$(published_port "$run-2" 2379/tcp)
CONF_TEST_ETCD_PEER_IP=$(peer_ip 1)
CONF_TEST_CONSUL=$(published_port "$run-consul" 8500/tcp)
for attempt in {1..60}; do
  if curl -fsS --max-time 2 "http://$CONF_TEST_ETCD/health" 2>/dev/null | grep -q true &&
     curl -fsS --max-time 2 "http://$CONF_TEST_CONSUL/v1/status/leader" 2>/dev/null | grep -q ':8300'; then break; fi
  if [[ "$attempt" == 60 ]]; then echo "clusters not ready" >&2; exit 1; fi
  sleep 1
done
for adapter in etcd consul; do
  report="$reports/$adapter.jsonl"
  testdata/ci/adapter.sh "$adapter" go test -race -count=1 -timeout=3m -tags=integration -json ./... > "$report" ||
    { cat "$report" >&2; exit 1; }
  python3 - "$adapter" "$report" <<'PY'
import json, sys
adapter, report = sys.argv[1:]
package = "github.com/apus-run/gala/components/conf/v2/sources/" + adapter
special = "LeaderRecovery" if adapter == "etcd" else "ACLFilteredPrefix"
required = {"TestIntegration" + case for case in ("Document", "Prefix", special)}
passed = set()
with open(report) as stream:
    for line in stream:
        event = json.loads(line)
        name = event.get("Test", "")
        if event.get("Package") != package or name.split("/")[0] not in required:
            continue
        if event["Action"] == "skip":
            sys.exit(f"{adapter}: skipped {name}")
        if event["Action"] == "pass":
            passed.add(name)
if required - passed:
    sys.exit(f"{adapter}: missing {sorted(required - passed)}")
print(f"{adapter}: {', '.join(sorted(required))} passed")
PY
done
