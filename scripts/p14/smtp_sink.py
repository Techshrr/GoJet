#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import socketserver
import threading


def empty_state() -> dict[str, object]:
    return {
        "connections": 0,
        "deliveries": 0,
        "transient_rejections": 0,
        "terminal_rejections": 0,
        "last_message_sha256": "",
    }


class State:
    def __init__(self, path: Path, mode_path: Path):
        self.path = path
        self.mode_path = mode_path
        self.lock = threading.Lock()
        self.value = empty_state()
        self.persist()

    def mode(self) -> str:
        try:
            value = self.mode_path.read_text(encoding="utf-8").strip()
        except FileNotFoundError:
            value = "success"
        return value if value in {"success", "transient", "terminal"} else "terminal"

    def update(self, **changes):
        with self.lock:
            # Each P14 case deletes the state file during fixture reset. Treat a
            # missing file as an explicit reset boundary so the long-running
            # protocol sink cannot leak counters from a prior case.
            if not self.path.exists():
                self.value = empty_state()
            for key, value in changes.items():
                if key.endswith("_increment"):
                    target = key.removesuffix("_increment")
                    self.value[target] = int(self.value.get(target, 0)) + int(value)
                else:
                    self.value[key] = value
            self.persist()

    def persist(self):
        self.path.parent.mkdir(parents=True, exist_ok=True)
        tmp = self.path.with_suffix(self.path.suffix + ".tmp")
        tmp.write_text(json.dumps(self.value, sort_keys=True) + "\n", encoding="utf-8")
        tmp.replace(self.path)


class Handler(socketserver.StreamRequestHandler):
    def send(self, line: str):
        self.wfile.write((line + "\r\n").encode("ascii"))
        self.wfile.flush()

    def handle(self):
        self.server.state.update(connections_increment=1)
        self.send("220 gojet-p14-smtp-sink ESMTP")
        while True:
            raw = self.rfile.readline(65537)
            if not raw or len(raw) > 65536:
                return
            line = raw.decode("utf-8", "replace").rstrip("\r\n")
            upper = line.upper()
            if upper.startswith("EHLO "):
                self.send("250-gojet-p14-smtp-sink")
                self.send("250 8BITMIME")
            elif upper.startswith("HELO "):
                self.send("250 gojet-p14-smtp-sink")
            elif upper.startswith("MAIL FROM:"):
                self.send("250 2.1.0 sender accepted")
            elif upper.startswith("RCPT TO:"):
                mode = self.server.state.mode()
                if mode == "transient":
                    self.server.state.update(transient_rejections_increment=1)
                    self.send("451 4.3.0 temporary local test failure")
                elif mode == "terminal":
                    self.server.state.update(terminal_rejections_increment=1)
                    self.send("550 5.1.1 terminal local test failure")
                else:
                    self.send("250 2.1.5 recipient accepted")
            elif upper == "DATA":
                self.send("354 End data with <CR><LF>.<CR><LF>")
                chunks: list[bytes] = []
                total = 0
                while True:
                    data = self.rfile.readline(262145)
                    if not data:
                        return
                    if data in {b".\r\n", b".\n"}:
                        break
                    total += len(data)
                    if total > 2 * 1024 * 1024:
                        self.send("552 5.3.4 message too large")
                        return
                    if data.startswith(b".."):
                        data = data[1:]
                    chunks.append(data)
                digest = hashlib.sha256(b"".join(chunks)).hexdigest()
                self.server.state.update(deliveries_increment=1, last_message_sha256=digest)
                self.send("250 2.0.0 queued locally")
            elif upper == "RSET":
                self.send("250 2.0.0 reset")
            elif upper == "NOOP":
                self.send("250 2.0.0 ok")
            elif upper == "QUIT":
                self.send("221 2.0.0 bye")
                return
            else:
                self.send("502 5.5.2 command not implemented")


class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True

    def __init__(self, address, state: State):
        self.state = state
        super().__init__(address, Handler)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=2525)
    parser.add_argument("--state", required=True)
    parser.add_argument("--mode-file", required=True)
    args = parser.parse_args()
    state = State(Path(args.state), Path(args.mode_file))
    with Server((args.host, args.port), state) as server:
        print(json.dumps({"status": "READY", "host": args.host, "port": args.port}), flush=True)
        server.serve_forever(poll_interval=0.1)


if __name__ == "__main__":
    main()
