# future work on moxie

## bootstrap completion

the immediate gate: binary distribution that rebuilds its own source. once this lands, the Go umbilical is cut for downstream users. current session is eliminating the imaginary number type as part of this pass.


## language changes

### moxiefmt

one-line single-statement bodies. optional inline semicolons for terse expression.

### int32 as canonical index/rune type

replaces the current split between int and rune. one integer type for indexing and character representation.

### deferred numeric types

128-bit fixed point, 192/64 fixed point, 256-bit integer. these come after bootstrap but before any serious crypto or financial work in pure moxie.

### internal package restriction removal

the Go-inherited `internal/` visibility rule adds complexity without value in a single-owner codebase model. remove it.


## stdlib surgery

### already done

- reflect: killed
- empty interface: killed
- string/[]byte: unified
- encoding/gob: out
- encoding/json: simplified for NIP-01
- go keyword: replaced by builtin spawn() + channels

### removal candidates

reflect-dependent packages are dead on arrival:

- fmt (moxiefmt replaces)
- database/sql
- text/template, html/template
- encoding/xml
- encoding/binary

other removals:

- context: redundant when spawn() controls lifetime. cancellation = done channel, deadlines = timers, value propagation was always an antipattern (typed map smuggled past the compiler).
- errors (wrapping chain): unwrap/Is/As is reflect-lite. replace with explicit error enums or codes.
- flag: reflect-based. trivial explicit replacement.
- sync.Map: no shared state in single-thread-per-process model. gone entirely.

### redesign candidates

- math/big: terrible API. full rewrite pending, currently blocked behind imaginary type removal.
- net/http: context-poisoned throughout, massively overdesigned. if Smesh is the transport, http becomes a compatibility shim at best.
- io.Reader/io.Writer: string/[]byte unification may change interface signatures. if it does, the change propagates into bufio, bytes, strings, and the io tree.

### maintenance

periodic sweep for deprecated Go stdlib API calls after any toolchain version bump.


## IPC: domain sockets over process tree

### transport

unix domain sockets with raw byte transfer. length prefix framing only. blocking send/receive - unbuffered channel semantics at the IPC boundary.

blocking send means the sender knows the receiver has the bytes when send() returns. the socket buffer provides natural backpressure without application-level flow control.

the transport interface is abstract: read(buf) -> n and write(buf) -> n. underlying mechanism could be domain socket, pipe, network socket, or shared memory. for local IPC, raw bytes over domain sockets is default.

### process invocation

single binary with subcommand-style entry points via os.Args: `./moxie relay`, `./moxie bridge`, `./moxie crawler`. no fork().

fork copies the parent's memory space, violating clean process separation. a child spawned via os.Args starts with a blank slate, receives its parent's socket path as an argument, connects back, and the relationship is established through the protocol. what crosses the boundary is hostile until proven. fork pretends the boundary doesn't exist.

### two channel types

vertical (stdin/stdout): parent spawns child, already has its stdin/stdout. this is the bootstrap channel - no negotiation needed. universal across POSIX and non-POSIX because stdin/stdout originates from C's universal process model. the parent uses this channel to send connection information (socket paths) to the child.

horizontal (domain sockets): once two processes have each other's socket path, they connect directly. all subsequent communication is peer-to-peer. the negotiation payload is just a string - the filesystem path.

### addressing

each process binds a domain socket at a filesystem path. the path is the address. the path is the capability. filesystem permissions provide access control - the security boundary is the OS, not the application.

### process tree as routing table

the process tree formed by spawn() is the discovery mechanism. the tree is used only for connection setup - once two processes have each other's socket paths, they communicate directly.

siblings (same parent): child A requests connection to B via stdin/stdout. parent looks up B's socket path, writes it to A's stdin. A connects directly to B's domain socket. peer-to-peer from there.

cousins (shared grandparent): request walks up to the nearest common ancestor and back down over the vertical channels. after handshake, the horizontal connection is direct.

grandparent+ routing requires a shared spawn tree that propagates as it changes - each ancestor must have a current view of the subtree below it. spawn/despawn notifications are the mechanism, but the tree must be consistent at every level for routing to work.

### worker pools

domains with multiple spawns for parallel bulk processing (e.g. a pool of compute workers). it doesn't matter which leaf gets a job - the parent acts as a dispatcher, sending to whichever child's channel is ready to receive. workers may be added and removed dynamically; the parent's child table is the pool registry. spawn adds a worker, despawn removes one. the parent absorbs the pool management naturally without a separate pool abstraction.

### spawn registration

when a process spawns a child, it creates a deterministic socket path, registers in its local child table, and sends a registration message up via stdin/stdout. registration propagates up to root.

each node maintains two tables: children (direct spawns with socket paths and pids) and descendants (routing table - "to reach process X, go through my child Y").

despawn: parent detects closed stdin/stdout, removes from child table, sends despawn notification up. ancestors remove routing entries. surviving peers on open domain sockets get connection-closed and handle it.

### properties

- no concurrency within a process. blocking send/receive. pure CSP.
- no fork(). single binary, os.Args subcommands. clean slate on spawn().
- vertical channel is free with spawn(). universal.
- horizontal channel is negotiated over the vertical channel.
- tree is for discovery only. data flows peer-to-peer after handshake.
- registration propagates up on spawn, down on lookup.
- no buffering. no hidden queues. send blocks until received.
- transport is abstract. domain sockets are default, not only option.
- security is filesystem permissions on socket paths.
- the path is the capability.


## test infrastructure

current workaround: _-import main to force compilation, tests in subdirectory with own main. full test runner implementation needed - not deferred, this is active work once bootstrap completes.


## visual identity

moxie logo (vertical ambigram, March 29 2026): all curves, reversed-E/euro-sign for M/E, interlocking arcs for X, curved i/o with dual-duty dot. needs clean SVG.


## graphics + shell (near horizon)

GLES/Vulkan canvas and IO system. moxie userspace compositor, Iskra lattice as shell (intent -> action, no UI chrome), Cap'n Proto IPC. drops onto any existing linux/android as a userspace layer. funded via 2-of-3 multisig escrow, no VCs/NDAs, per-phase. pending runway.


## kernel (far horizon)

moxie rewrite of linux kernel modeling BSD, powered by Iskra. targets any hardware that runs a linux or android kernel. the compositor/shell layer above is the proving ground - kernel work starts when that layer is stable and the language is self-hosting deep enough to write a kernel in.

### boot contract

boot-compatible with existing linux bootloaders (grub, systemd-boot, u-boot on android). same bzImage/initramfs contract. meet the boot format, do whatever you want inside.

### picokernel architecture

no conventional scheduler. each hardware thread runs a picokernel that owns its own memory and communicates sideways via cache lines as pipes. cache coherency protocol (MESI/MOESI) provides blocking transport with ordering and visibility guarantees - unbuffered channel semantics from silicon.

no shared memory, no locks, no atomics. "scheduling" is just reading the next message from the queue. priority is implicit in channel selection order. load balancing is spawn placement - put work where the data already lives in cache.

cores don't migrate. work migrates by re-spawning on idle cores and rerouting messages. same pattern as the IPC process tree.

### hardware domain cores

hardware resources (interrupt controller, DMA, IOMMU, physical page allocator) are inherently singular. dedicated cores own device domains: NIC interrupts on one core, storage DMA on another, GPU/display on another. each is singular for its domain - ownership unambiguous.

migration for hardware-domain cores uses idle windows dictated by each domain's IO rhythm: vsync for display, write barrier completion for storage, interrupt coalescing intervals for network. migration trigger: queue empty AND thermal above threshold. no global thermal governor.

each domain's migration rhythm must be explicitly implemented in its picokernel - there is no generic migration driver. the display core knows about vsync, the storage core knows about write barriers, the NIC core knows about coalescing. the migration logic is domain-specific code, not a shared scheduler policy. this is unavoidable: the idle window detection is coupled to the domain's protocol.

pure compute cores never migrate - thermally managed by natural backpressure from blocking sends.

### programmable logic (FPGA) path

the picokernel model is the software emulation of what lattice geometry processors do structurally. on FPGA/programmable logic, the topology IS the scheduler - every node is both compute and routing, hotspots can't form because path through the lattice is the computation.

a background metrics worker (itself a picokernel) gathers path efficiency data, evaluates whether topology reconfiguration is worthwhile, and schedules reconfiguration during idle windows. on FPGA this means rerouting actual gates - drain-migrate-resume applied one level below software. same architecture at every level, no special case.

FPGA exploration comes first to discover optimal patterns before committing to fixed silicon. the natural parallelism of lattice processing may make the reconfiguration worker's job trivial - periodic evaluation rather than constant intervention.
