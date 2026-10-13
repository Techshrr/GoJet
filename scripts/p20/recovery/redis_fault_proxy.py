"""Transparent Redis transport fault injection, never a Redis response emulator.

Only command names and connection/fault counts are retained. Original RESP frames
and replies pass to/from the actual Redis server or the TCP connection is closed.
"""
import socket
import socketserver
import threading


def command_frame(stream):
    """Bounded RESP command-array parser (go-redis sends bulk string arrays)."""
    first = stream.readline(128)
    if not first:
        return None, None
    if not first.startswith(b'*') or not first.endswith(b'\r\n'):
        raise ValueError('invalid Redis command framing')
    count = int(first[1:-2])
    if not 1 <= count <= 4096:
        raise ValueError('invalid Redis command length')
    chunks, name, total = [first], None, len(first)
    for index in range(count):
        header = stream.readline(128)
        if not header.startswith(b'$') or not header.endswith(b'\r\n'):
            raise ValueError('invalid Redis bulk framing')
        length = int(header[1:-2])
        total += len(header) + length + 2
        if length < 0 or total > 4 * 1024 * 1024:
            raise ValueError('oversized Redis command')
        body = stream.read(length + 2)
        if len(body) != length + 2 or not body.endswith(b'\r\n'):
            raise ValueError('truncated Redis command')
        if index == 0:
            name = body[:-2].upper()
        chunks.extend((header, body))
    return name, b''.join(chunks)


def close_socket(sock):
    try:
        sock.shutdown(socket.SHUT_RDWR)
    except OSError:
        pass
    sock.close()


class Handler(socketserver.BaseRequestHandler):
    def handle(self):
        proxy = self.server
        upstream = None
        try:
            upstream = socket.create_connection(proxy.target, timeout=5)
            upstream.settimeout(None)
            with proxy.lock:
                proxy.connections += 1
                proxy.sockets.update((self.request, upstream))
            def replies():
                try:
                    while True:
                        data = upstream.recv(65536)
                        if not data:
                            break
                        self.request.sendall(data)
                except OSError:
                    pass
                finally:
                    close_socket(self.request)
            reply_thread = threading.Thread(target=replies, daemon=True)
            reply_thread.start()
            with self.request.makefile('rb') as stream:
                while True:
                    name, frame = command_frame(stream)
                    if frame is None:
                        break
                    with proxy.lock:
                        blocked = proxy.block_all or name in proxy.blocked
                        if blocked:
                            proxy.faults += 1
                            if name in (b'XADD', b'XACK'):
                                key = name.decode('ascii')
                                proxy.blocked_commands[key] = proxy.blocked_commands.get(key, 0) + 1
                    if blocked:
                        break
                    upstream.sendall(frame)
        except (OSError, ValueError):
            # A framing failure is an observable test failure, never a fabricated reply.
            with proxy.lock:
                proxy.transport_errors += 1
        finally:
            if upstream is not None:
                close_socket(upstream)
            close_socket(self.request)
            with proxy.lock:
                proxy.sockets.discard(self.request)
                proxy.sockets.discard(upstream)


class RedisFaultProxy(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True

    def __init__(self, target, address=('127.0.0.1', 0)):
        self.target = target
        self.lock = threading.Lock()
        self.block_all = False
        self.blocked = set()
        self.sockets = set()
        self.connections = self.faults = self.transport_errors = 0
        self.blocked_commands = {}
        super().__init__(address, Handler)
        self.thread = threading.Thread(target=self.serve_forever, daemon=True)
        self.thread.start()

    def set_fault(self, commands=(), all_connections=False):
        with self.lock:
            self.blocked = {x.upper().encode('ascii') for x in commands}
            self.block_all = all_connections
            current = list(self.sockets) if all_connections else []
        for sock in current:
            close_socket(sock)

    def snapshot(self):
        with self.lock:
            return {'connections':self.connections,'faults':self.faults,
                    'transport_errors':self.transport_errors,'blocked_commands':dict(self.blocked_commands)}

    def stop(self):
        self.shutdown()
        self.set_fault(all_connections=True)
        self.server_close()
        self.thread.join(timeout=5)
