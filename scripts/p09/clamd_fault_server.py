#!/usr/bin/env python3
from __future__ import annotations

import argparse
import datetime as dt
import json
import socketserver
import struct
import time


def version_reply(stale: bool) -> bytes:
    current = dt.datetime(2000, 1, 1, 0, 0, 0) if stale else dt.datetime.now(dt.timezone.utc).replace(tzinfo=None)
    date = f"{current:%a %b} {current.day} {current:%H:%M:%S %Y}"
    return f"ClamAV 1.4.3/99999/{date}\0".encode()


def read_exact(stream, size: int) -> bytes:
    data = b""
    while len(data) < size:
        chunk = stream.read(size - len(data))
        if not chunk:
            raise EOFError("client disconnected")
        data += chunk
    return data


class Handler(socketserver.StreamRequestHandler):
    def handle(self):
        command = bytearray()
        while True:
            char = self.rfile.read(1)
            if not char:
                return
            if char == b"\0":
                break
            command.extend(char)
            if len(command) > 1024:
                return
        command_text = bytes(command).decode("ascii", "replace").lstrip("z")
        if command_text == "VERSION":
            if self.server.mode == "health-timeout":
                time.sleep(self.server.hold_seconds)
                try:
                    self.wfile.write(version_reply(False))
                    self.wfile.flush()
                except OSError:
                    pass
                return
            if self.server.mode == "health-indeterminate":
                self.wfile.write(b"not-a-clamav-version\0")
                self.wfile.flush()
                return
            self.wfile.write(version_reply(self.server.mode == "stale"))
            self.wfile.flush()
            return
        if command_text != "INSTREAM":
            self.wfile.write(b"UNKNOWN COMMAND\0")
            self.wfile.flush()
            return
        try:
            while True:
                size_raw = read_exact(self.rfile, 4)
                size = struct.unpack(">I", size_raw)[0]
                if size == 0:
                    break
                read_exact(self.rfile, size)
        except (EOFError, ConnectionError, OSError):
            return
        if self.server.mode in {"timeout", "hold"}:
            time.sleep(self.server.hold_seconds)
            try:
                self.wfile.write(b"stream: OK\0")
                self.wfile.flush()
            except OSError:
                pass
            return
        if self.server.mode == "indeterminate":
            self.wfile.write(b"stream: UNKNOWN\0")
            self.wfile.flush()
            return
        self.wfile.write(b"stream: ERROR\0")
        self.wfile.flush()


class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True

    def __init__(self, address, mode: str, hold_seconds: float):
        self.mode = mode
        self.hold_seconds = hold_seconds
        super().__init__(address, Handler)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--mode",
        required=True,
        choices=["timeout", "stale", "indeterminate", "hold", "health-timeout", "health-indeterminate"],
    )
    parser.add_argument("--port", required=True, type=int)
    parser.add_argument("--hold-seconds", type=float, default=2.0)
    args = parser.parse_args()
    with Server(("127.0.0.1", args.port), args.mode, args.hold_seconds) as server:
        print(json.dumps({"status": "READY", "mode": args.mode, "port": args.port}), flush=True)
        server.serve_forever(poll_interval=0.1)


if __name__ == "__main__":
    main()
