#!/usr/bin/env bash
set -euo pipefail

BINARY=${1:?built perk-workbench binary is required}
if [[ ! -f "$BINARY" || ! -s "$BINARY" ]]; then
  echo "missing or empty built binary: $BINARY" >&2
  exit 1
fi

python_bin=python3
command -v "$python_bin" >/dev/null 2>&1 || python_bin=python
"$python_bin" - "$BINARY" <<'PY'
import json
import queue
import subprocess
import sys
import threading

binary = sys.argv[1]
timeout = 20.0


def read_line(stream, result):
    try:
        result.put(stream.readline())
    except BaseException as exc:
        result.put(exc)


def response(process, request):
    process.stdin.write((json.dumps(request, separators=(",", ":")) + "\n").encode())
    process.stdin.flush()
    result = queue.Queue(maxsize=1)
    threading.Thread(target=read_line, args=(process.stdout, result), daemon=True).start()
    try:
        line = result.get(timeout=timeout)
    except queue.Empty:
        raise RuntimeError("timed out waiting for JSON-RPC response")
    if isinstance(line, BaseException):
        raise line
    if not line:
        raise RuntimeError("plugin exited before returning a response")
    try:
        return json.loads(line)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"invalid stdout frame {line!r}: {exc}")


def run_mode(mode):
    process = subprocess.Popen(
        [binary, "--plugin", mode],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    try:
        initialized = response(process, {
            "jsonrpc": "2.0",
            "id": 1,
            "method": "perk/v1/initialize",
            "params": {"protocol_version": 1, "workbench_version": "release-smoke"},
        })
        capabilities = initialized.get("result", {}).get("capabilities", {})
        if initialized.get("id") != 1 or capabilities.get("name") != mode or capabilities.get("driver") != mode:
            raise RuntimeError(f"{mode}: unexpected initialize response {initialized!r}")
        if mode == "sqlite":
            opened = response(process, {
                "jsonrpc": "2.0",
                "id": 2,
                "method": "perk/v1/open",
                "params": {"target": ":memory:"},
            })
            session = opened.get("result", {}).get("session_id")
            if opened.get("id") != 2 or not isinstance(session, int) or session < 1:
                raise RuntimeError(f"sqlite: unexpected open response {opened!r}")
            closed = response(process, {
                "jsonrpc": "2.0",
                "id": 3,
                "method": "perk/v1/close",
                "params": {"session_id": session},
            })
            if closed.get("id") != 3 or "error" in closed:
                raise RuntimeError(f"sqlite: unexpected close response {closed!r}")
    except BaseException as exc:
        try:
            process.kill()
        finally:
            _, stderr = process.communicate(timeout=timeout)
        detail = stderr.decode(errors="replace").strip()
        raise RuntimeError(f"{mode}: {exc}; stderr={detail!r}")
    finally:
        if process.stdin:
            process.stdin.close()
        try:
            process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=timeout)
    if process.returncode != 0:
        stderr = process.stderr.read().decode(errors="replace").strip()
        raise RuntimeError(f"{mode}: exit status {process.returncode}; stderr={stderr!r}")


for mode in ("sqlite", "mysql", "postgres", "mongodb"):
    run_mode(mode)
    print(f"self-plugin smoke passed: {mode}")
PY
