# Moxie IPC: Domain Sockets over Process Tree

## Transport

Unix domain sockets with raw byte transfer. No serialization framing beyond a length prefix. Blocking send/receive — unbuffered channel semantics. No buffered channels over Spawn. Pure CSP at the IPC boundary.

Blocking send means the sender knows the receiver has the bytes when send() returns. No hidden state, no race conditions, no ambiguity. The socket buffer provides natural backpressure without application-level flow control.

The transport layer is abstract: the interface is read(buf) → n and write(buf) → n. The underlying mechanism could be a domain socket, a pipe, a network socket, or shared memory. Callers don't know and don't care. For local IPC, raw bytes over domain sockets is the default — thinnest possible layer with blocking backpressure.

## Process Invocation

Single binary with subcommand-style entry points via os.Args: `./moxie relay`, `./moxie bridge`, `./moxie crawler`. No fork(). Fork is a Unix-ism that doesn't exist on Windows, doesn't exist in WASM, and doesn't map to any hypothetical future Moxie-native OS.

Fork also copies the parent's memory space, violating clean process separation. A child spawned via os.Args starts with a blank slate, receives its parent's socket path as an argument, connects back, and the relationship is established through the protocol, not through inherited state. What crosses the boundary is hostile until proven. Fork pretends the boundary doesn't exist.

## Two Channel Types

### Vertical: stdin/stdout (parent ↔ child)

The parent spawns the child and already has its stdin/stdout. This is the bootstrap channel — no negotiation needed. It comes free with the spawn and works on anything that has processes and file descriptors. This is universal across POSIX and non-POSIX systems because stdin/stdout originates from C's universal process model.

The parent uses the vertical channel to send connection information (socket paths) to the child when sibling or cousin connections are requested.

### Horizontal: domain sockets (sibling, cousin, any peer)

Once two processes have each other's socket path, they connect directly via domain socket. All subsequent communication is peer-to-peer. The negotiation payload is just a string — the filesystem path. The connecting process calls connect(path) and from that point on it's a live bidirectional byte stream.

## Addressing

Each process binds a domain socket at a filesystem path. The path is the address. The path is the capability. Filesystem permissions provide access control — the security boundary is the OS, not the application.

## Process Tree as Routing Table

The process tree formed by Spawn is the discovery mechanism. The tree is used only for connection setup — once two processes have each other's socket paths, they communicate directly. No ongoing routing through the tree.

### Siblings (same parent)

1. Child A sends a connection request to its parent via stdin/stdout: "I need to talk to B"
2. Parent looks up B's socket path in its child registry
3. Parent writes B's socket path to A's stdin
4. A connects directly to B's domain socket
5. All subsequent communication is peer-to-peer over the domain socket

### Cousins (shared grandparent)

1. Child A sends a connection request to its parent via stdin/stdout: "I need to talk to C"
2. Parent does not have C — forwards the request up to the grandparent via its own stdin/stdout
3. Grandparent looks up C's lineage, identifies C's parent (uncle of A)
4. Grandparent writes the request down to C's parent via stdin/stdout
5. C's parent returns C's socket path back up to grandparent
6. Grandparent returns the path back down through A's parent to A
7. A connects directly to C's domain socket

The request walks up to the nearest common ancestor and back down over the vertical stdin/stdout channels. After handshake, the horizontal connection is direct.

## Spawn Registration: How Ancestors Know About Descendants

When a process spawns a child:

1. The parent creates the child's domain socket path (deterministic, based on parent path + child index or name)
2. The parent registers the child in its local child table: {name → socket_path, pid}
3. The parent sends a registration message up to its own parent via stdin/stdout: "I spawned child X at path Y"
4. The grandparent adds X to its descendant table, tagged with the intermediate parent
5. Registration propagates up to the root

Each node in the tree maintains two tables:

- children: direct spawns, with socket paths and pids
- descendants: indirect spawns reported from below, with the route through the tree to reach them

The descendant table is not a full copy of the subtree — it's a routing table. Each entry says "to reach process X, go through my child Y." The actual socket path resolution happens by walking down.

### Spawn notification message

When child B spawns grandchild C:

```
B → Parent (via stdout): { type: "spawn", name: "C", path: "/tmp/moxie/B/C.sock" }
Parent → Grandparent (via stdout): { type: "spawn", name: "B/C", via: "B" }
```

The grandparent stores: name "B/C" → route through child "B". If A asks for "B/C", grandparent knows to forward to B.

### Despawn

Same path in reverse. When a process exits, its parent detects the closed stdin/stdout, removes it from the child table, and sends a despawn notification up. Ancestors remove the routing entry. Any open direct domain sockets to the dead process get a connection-closed error — the surviving peer handles reconnection or failure as appropriate.

## Properties

- No concurrency within a process. Blocking send/receive. Pure CSP.
- No fork(). Single binary, os.Args subcommands. Clean slate on spawn.
- Vertical channel (stdin/stdout) is free with the spawn. Universal across all OSs.
- Horizontal channel (domain sockets) is negotiated over the vertical channel.
- The tree is for discovery only. Data flows peer-to-peer after handshake.
- Registration propagates up on spawn, down on lookup.
- No buffering. No hidden queues. Send blocks until received.
- Transport is abstract. Domain sockets are the default, not the only option.
- Security is filesystem permissions on socket paths.
- The negotiation payload is just a socket path string. The path is the capability.
