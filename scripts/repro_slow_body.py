#!/usr/bin/env python3
"""Reproduce premature completion using an unmodified Gopeed executable only.

No DSM, SDK, or Gopeed package adapter is involved. Output contains local fixture
URLs and engine state, never the private API token. Requires only Python stdlib.
"""
import argparse
import concurrent.futures
import hashlib
import http.server
import json
import pathlib
import secrets
import socket
import subprocess
import threading
import time
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--delay", type=float, default=45)
    args = parser.parse_args()
    if not 0 <= args.delay <= 60:
        parser.error("delay must be between 0 and 60 seconds")
    output = pathlib.Path(args.output).resolve()
    output.mkdir(parents=True, exist_ok=False)
    payload = bytes((i % 251) + 1 for i in range(41984))
    stop = threading.Event()
    requests = []

    class Source(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            started = time.monotonic()
            self.send_response(200)
            self.send_header("Content-Length", str(len(payload)))
            self.send_header("Content-Type", "application/octet-stream")
            self.end_headers()
            self.wfile.flush()
            event = {"path": self.path, "delay": args.delay}
            requests.append(event)
            if not stop.wait(args.delay):
                try:
                    self.wfile.write(payload)
                    event["bodySent"] = True
                except (BrokenPipeError, ConnectionResetError):
                    event["bodySent"] = False
            event["elapsed"] = time.monotonic() - started

        def log_message(self, *_):
            pass

    source = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Source)
    threading.Thread(target=source.serve_forever, daemon=True).start()
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        port = probe.getsockname()[1]
    token = secrets.token_hex(32)
    stage = output / "staging"
    stage.mkdir()
    job_stage = stage / "job"
    job_stage.mkdir()
    config = output / "engine.json"
    config.write_text(json.dumps({"address": "127.0.0.1", "port": port,
                                 "apiToken": token, "storageDir": str(output / "storage"),
                                 "whiteDownloadDirs": [str(stage / "*")]}))
    config.chmod(0o600)

    def engine(method, path, data=None):
        body = None if data is None else json.dumps(data).encode()
        req = urllib.request.Request(f"http://127.0.0.1:{port}{path}", body,
                                     {"X-Api-Token": token, "Content-Type": "application/json"},
                                     method=method)
        with urllib.request.urlopen(req, timeout=5) as response:
            result = json.load(response)
        if result["code"] != 0:
            raise RuntimeError(result)
        return result["data"]

    def independent_read():
        start = time.monotonic()
        with urllib.request.urlopen(f"http://127.0.0.1:{source.server_port}/independent.bin",
                                    timeout=args.delay + 10) as response:
            body = response.read()
        return {"elapsed": time.monotonic() - start, "bytes": len(body),
                "sha256": hashlib.sha256(body).hexdigest(), "matches": body == payload}

    logfile = (output / "engine.log").open("wb")
    process = subprocess.Popen([str(pathlib.Path(args.binary).resolve()), "-c", str(config)],
                               stdout=logfile, stderr=logfile)
    try:
        for _ in range(100):
            try:
                info = engine("GET", "/api/v1/info")
                break
            except (OSError, ValueError):
                time.sleep(.05)
        else:
            raise RuntimeError("engine did not start")
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
            independent = pool.submit(independent_read)
            start = time.monotonic()
            task_id = engine("POST", "/api/v1/tasks", {
                "req": {"url": f"http://127.0.0.1:{source.server_port}/delayed.bin"},
                "opts": {"path": str(job_stage), "name": "delayed.bin"}})
            samples = []
            while time.monotonic() - start < args.delay + 20:
                task = engine("GET", "/api/v1/tasks/" + task_id)
                stats = engine("GET", "/api/v1/tasks/" + task_id + "/stats")
                samples.append({"elapsed": time.monotonic() - start, "task": task, "stats": stats})
                if task["status"] in ("done", "error"):
                    break
                time.sleep(.25)
            body = (job_stage / "delayed.bin").read_bytes()
            result = {"officialBinarySHA256": hashlib.sha256(pathlib.Path(args.binary).read_bytes()).hexdigest(),
                      "info": info, "delay": args.delay, "expectedBytes": len(payload),
                      "expectedSHA256": hashlib.sha256(payload).hexdigest(), "terminal": samples[-1],
                      "file": {"bytes": len(body), "sha256": hashlib.sha256(body).hexdigest(),
                               "nonzeroBytes": sum(b != 0 for b in body), "matches": body == payload},
                      "independentRead": independent.result(), "requests": requests}
            (output / "samples.json").write_text(json.dumps(samples, indent=2) + "\n")
            (output / "result.json").write_text(json.dumps(result, indent=2) + "\n")
            print(json.dumps(result, indent=2))
    finally:
        stop.set()
        source.shutdown()
        source.server_close()
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
        logfile.close()
        config.unlink()  # Do not retain the fixture's token.


if __name__ == "__main__":
    main()
