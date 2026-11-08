#!/usr/bin/env bash
# End-to-end test of the real binary, run under strace to prove what the
# process does on the network. Needs: go, curl, python3, strace.
#
# Part 1 proves no outbound connections with embeddings off, including from
# third-party code, which egress_test.go cannot see.
# Part 2 is the positive control: with embeddings on, the same tracing must
# see connections, and only to the configured endpoint. Without it, an empty
# trace could just mean the tracing was broken.
set -euo pipefail
repo="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
pids=()
# Killing strace only detaches it and leaves the traced process running, so
# stop the child first.
stop() { pkill -TERM -P "$1" 2>/dev/null || true; kill "$1" 2>/dev/null || true; wait "$1" 2>/dev/null || true; }
cleanup() { for p in "${pids[@]:-}"; do [ -n "$p" ] && stop "$p"; done; rm -rf "$work"; }
trap cleanup EXIT
pass=0
ok()   { pass=$((pass + 1)); echo "  ok: $1"; }
fail() {
  echo "  FAIL: $1"; [ -n "${2:-}" ] && echo "$2" | head -20
  echo "--- server logs"; tail -n 8 "$work"/serve*.log 2>/dev/null | cut -c1-240
  echo "--- status"; "$work/roost" status --config "$work/roost.toml" 2>&1 | head -24 | cut -c1-200
  exit 1
}
has()  { case "$2" in *"$3"*) ok "$1";; *) fail "$1" "$2";; esac; }
hasnt(){ case "$2" in *"$3"*) fail "$1" "$2";; *) ok "$1";; esac; }

cd "$repo"
CGO_ENABLED=0 go build -trimpath -o "$work/roost" ./cmd/roost
fx="$repo/internal/extract/testdata"
mkdir -p "$work/legal/deals" "$work/hr"
cp "$fx/sample.docx" "$fx/sample.pptx" "$fx/multipage.pdf" "$work/legal/deals/"
cp "$fx/sample.xlsx" "$fx/scanned.pdf" "$fx/encrypted.pdf" "$work/hr/"
echo "payroll secret kestrel bands" > "$work/hr/salaries.txt"
echo "legacy" > "$work/legal/old.doc"
head -c 4096 /dev/urandom > "$work/legal/corrupt.docx"
echo "outside the collection" > "$work/outside.txt"
ln -s "$work/outside.txt" "$work/legal/escape.txt"

port=18484
cat > "$work/roost.toml" <<T
[server]
listen = "127.0.0.1:$port"
[storage]
data_dir = "$work/data"
[index]
watch_debounce = "200ms"
[audit]
jsonl_path = "$work/audit.jsonl"
[[collections]]
name = "legal"
path = "$work/legal"
[[collections]]
name = "hr"
path = "$work/hr"
T
R() { "$work/roost" "$@"; }
legal=$(R token create --config "$work/roost.toml" --name legal-bot --collections legal 2>/dev/null)
admin=$(R token create --config "$work/roost.toml" --name admin --all 2>/dev/null)

wait_up() { for _ in $(seq 100); do curl -sf "http://127.0.0.1:$1/healthz" >/dev/null && return 0; sleep 0.1; done; fail "server did not start"; }
wait_for() { for _ in $(seq 150); do eval "$2" && { ok "$1"; return 0; }; sleep 0.1; done; fail "$1"; }
api() { curl -s -H "Authorization: Bearer $1" "${@:3}" "http://127.0.0.1:$port$2"; }
search() { api "$1" /v1/search -X POST -d "{\"query\":\"$2\"}"; }
mcp() { curl -s -H "Authorization: Bearer $1" -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -d "$2" "http://127.0.0.1:$port/mcp"; }
tool() { mcp "$1" "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"$2\",\"arguments\":$3}}"; }

echo "== Part 1: default configuration"
strace -f -qq -e trace=execve,connect,sendto,sendmsg -o "$work/trace.log" "$work/roost" serve --config "$work/roost.toml" 2>"$work/serve.log" &
pids+=($!)
wait_up "$port"
wait_for "initial index finished" '[ "$(R status --config "$work/roost.toml" | awk "/^legal/{print \$2}")" = "3" ]'
has "startup declares no egress" "$(cat "$work/serve.log")" "egress: none"

out=$(search "$legal" 'heron milestone')
has "finds text in a Word document" "$out" "sample.docx"
has "cites the Word section" "$out" '"section":"Risks"'
has "finds speaker notes" "$(search "$legal" kestrel)" "sample.pptx"
has "cites the PDF page" "$(search "$legal" goshawk)" '"section":"Page 2"'
has "finds spreadsheet cells for admin" "$(search "$admin" APAC)" "sample.xlsx"
hasnt "legal token cannot see HR" "$(search "$legal" 'payroll')" "salaries"
hasnt "symlink out of the collection not indexed" "$(api "$admin" /v1/documents)" "escape.txt"
st=$(R status --config "$work/roost.toml")
has "scanned PDF flagged" "$st" "no_text"
has "encrypted PDF flagged" "$st" "encrypted"
has "legacy format flagged" "$st" "unsupported"
has "corrupt file flagged" "$st" "failed"

id=$(search "$legal" goshawk | python3 -c 'import json,sys;print(json.load(sys.stdin)["results"][0]["doc_id"])')
has "reads one page by section" "$(api "$legal" "/v1/documents/$id/text?section=Page%202")" "goshawk"
hasnt "section read excludes other pages" "$(api "$legal" "/v1/documents/$id/text?section=Page%202")" "merlin"
hr=$(search "$admin" payroll | python3 -c 'import json,sys;print(json.load(sys.stdin)["results"][0]["doc_id"])')
[ "$(api "$legal" "/v1/documents/$hr/file" -o /dev/null -w '%{http_code}')" = 404 ] && ok "forbidden download is 404" || fail "forbidden download"
curl -s -H "Authorization: Bearer $legal" -o "$work/dl.pdf" "http://127.0.0.1:$port/v1/documents/$id/file"
cmp -s "$work/dl.pdf" "$fx/multipage.pdf" && ok "download matches the original bytes" || fail "download differs"

init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"e2e","version":"1"}}}'
has "MCP HTTP initializes" "$(mcp "$legal" "$init")" '"name":"roost"'
has "MCP HTTP lists tools" "$(mcp "$legal" '{"jsonrpc":"2.0","id":2,"method":"tools/list"}')" "search_documents"
has "MCP HTTP search works" "$(tool "$legal" search_documents '{"query":"goshawk"}')" "multipage.pdf"
hasnt "MCP HTTP honors the ACL" "$(tool "$legal" search_documents '{"query":"payroll"}')" "salaries"
[ "$(curl -s -o /dev/null -w '%{http_code}' -d "$init" "http://127.0.0.1:$port/mcp")" = 401 ] && ok "MCP without a token is 401" || fail "MCP auth"

# stdin stays open briefly, as with a real client: EOF ends the session.
reply=$( (printf '%s\n' "$init" '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search_documents","arguments":{"query":"goshawk"}}}'; sleep 1) \
  | (ROOST_TOKEN="$legal" timeout 5 strace -f -qq -e trace=connect,sendto,sendmsg -o "$work/trace-stdio.log" "$work/roost" mcp --config "$work/roost.toml" 2>/dev/null || true))
has "MCP stdio search works" "$reply" "multipage.pdf"
if env -u ROOST_TOKEN "$work/roost" mcp --config "$work/roost.toml" </dev/null >/dev/null 2>&1; then fail "stdio ran with no token"; else ok "stdio refuses to start with no token"; fi

echo "new sparrowhawk report" > "$work/legal/fresh.txt"
wait_for "new file becomes searchable" 'search "$legal" sparrowhawk | grep -q fresh.txt'
echo "rewritten with nightjar instead" > "$work/legal/fresh.txt"
wait_for "edited text becomes searchable" 'search "$legal" nightjar | grep -q fresh.txt'
wait_for "old text disappears after an edit" '! search "$legal" sparrowhawk | grep -q fresh.txt'
mv "$work/legal/fresh.txt" "$work/legal/moved.txt"
wait_for "rename follows" 'search "$legal" nightjar | grep -q moved.txt'
rm "$work/legal/moved.txt"
wait_for "deleted file leaves the index" '! search "$legal" nightjar | grep -q moved.txt'

tid=$(R token list --config "$work/roost.toml" | awk '/legal-bot/{print $1}')
R token revoke --config "$work/roost.toml" "$tid" >/dev/null
[ "$(api "$legal" /v1/collections -o /dev/null -w '%{http_code}')" = 401 ] && ok "revocation is immediate" || fail "revocation"

aud=$(R audit --config "$work/roost.toml" -n 500)
has "audit shows token names" "$aud" "legal-bot"
has "audit shows MCP HTTP" "$aud" "mcp-http"
has "audit shows MCP stdio" "$aud" "mcp-stdio"
has "audit shows downloads" "$aud" "download"
hasnt "no token in the database audit" "$aud" "roost_"
hasnt "no token in the JSONL audit" "$(cat "$work/audit.jsonl")" "roost_"
hasnt "no token in server logs" "$(cat "$work/serve.log")" "roost_"
[ "$(stat -c %a "$work/data")" = 700 ] && ok "data directory is 0700" || fail "data dir mode"
[ "$(stat -c %a "$work/data/roost.db")" = 600 ] && ok "database is 0600" || fail "db mode"

stop "${pids[0]}"
for f in trace.log trace-stdio.log; do
  net=$(grep -E 'AF_INET|AF_INET6|sin_port|sin6_port' "$work/$f" | grep -v 'resumed' || true)
  [ -z "$net" ] && ok "$f: zero network connections ($(wc -l <"$work/$f") lines traced)" || fail "$f shows network use" "$net"
done
# An empty trace only counts if the parsers really ran under it.
workers=$(grep -c 'execve(.*extract-worker' "$work/trace.log" || true)
[ "$workers" -ge 8 ] && ok "trace covered $workers extraction workers" || fail "only $workers workers traced"

echo "== Part 2: positive control, embeddings on"
eport=18485
cat > "$work/mock.py" <<'P'
import json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer
class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_POST(self):
        req = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        data = [{"index": i, "embedding": [1.0 if "goshawk" in t or "raptor" in t else 0.0, 0.5, 0.25]} for i, t in enumerate(req["input"])]
        body = json.dumps({"data": data}).encode()
        self.send_response(200); self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
HTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
P
python3 -I "$work/mock.py" "$eport" & pids+=($!)
for _ in $(seq 100); do (exec 3<>"/dev/tcp/127.0.0.1/$eport") 2>/dev/null && break; sleep 0.1; done
cat >> "$work/roost.toml" <<T
[embeddings]
enabled = true
endpoint = "http://127.0.0.1:$eport/v1/embeddings"
model = "mock"
T
strace -f -qq -e trace=connect -o "$work/trace-embed.log" "$work/roost" serve --config "$work/roost.toml" 2>"$work/serve2.log" &
pids+=($!)
wait_up "$port"
wait_for "all chunks embedded" 'R status --config "$work/roost.toml" | grep -E "Embeddings: ([1-9][0-9]*) of \1 chunks" >/dev/null'
has "startup names the endpoint" "$(cat "$work/serve2.log")" "egress: embeddings endpoint only"
sleep 0.2
r=$(search "$admin" raptor)
has "hybrid search reports semantic mode" "$r" "semantic"
has "semantic match finds text without the keyword" "$r" "multipage.pdf"
ports=$(grep -oE 'sin_port=htons\([0-9]+\)' "$work/trace-embed.log" | sort -u)
[ "$ports" = "sin_port=htons($eport)" ] && ok "tracing saw connections, only to the endpoint" || fail "unexpected destinations" "$ports"
stop "${pids[-1]}"; kill "${pids[-2]}" 2>/dev/null || true; wait "${pids[-2]}" 2>/dev/null || true

# The mock is gone now, so this server has an unreachable endpoint.
"$work/roost" serve --config "$work/roost.toml" 2>/dev/null &
pids+=($!)
wait_up "$port"
r=$(search "$admin" goshawk)
has "embeddings outage falls back to keywords" "$r" "multipage.pdf"
has "outage is reported to the client" "$r" "keyword results only"

stop "${pids[-1]}"

echo "== Part 3: listener safety"
openssl req -x509 -newkey rsa:2048 -nodes -keyout "$work/key.pem" -out "$work/cert.pem" -days 1 -subj "/CN=localhost" \
  -addext "subjectAltName=DNS:localhost" 2>/dev/null
sed -e '/^\[embeddings\]/,$d' -e "s/^listen = .*/listen = \"0.0.0.0:$port\"/" "$work/roost.toml" > "$work/open.toml"
if out=$("$work/roost" serve --config "$work/open.toml" 2>&1); then fail "served plaintext on a public address"; fi
has "non-loopback plaintext is refused" "$out" "not loopback"
sed "s/^\[server\]/[server]\ntls_cert = \"${work//\//\\/}\/cert.pem\"\ntls_key = \"${work//\//\\/}\/key.pem\"/" "$work/open.toml" > "$work/tls.toml"
"$work/roost" serve --config "$work/tls.toml" 2>/dev/null &
pids+=($!)
for _ in $(seq 100); do curl -sf --cacert "$work/cert.pem" "https://localhost:$port/healthz" >/dev/null && break; sleep 0.1; done
has "TLS serves search" "$(curl -s --cacert "$work/cert.pem" -H "Authorization: Bearer $admin" -d '{"query":"goshawk"}' "https://localhost:$port/v1/search")" "multipage.pdf"
if curl -s --tls-max 1.1 --insecure "https://localhost:$port/healthz" >/dev/null 2>&1; then fail "TLS 1.1 accepted"; else ok "TLS below 1.2 is refused"; fi

echo; echo "PASS: $pass checks"
