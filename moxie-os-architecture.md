# moxie OS: barrelfish redesigned for CSP domains

## the premise

barrelfish proved that treating a multicore machine as a network of independent cores with message passing is both correct and performant. their SOSP 2009 paper showed that message-passing OS structure matches or beats shared-memory kernels even on cache-coherent hardware, and scales better as core counts increase.

barrelfish still carries legacy from the unix/seL4 tradition: dispatchers, monitors, a central capability system, and RPC semantics inherited from the client-server model. moxie's spawn/channel/domain model eliminates all of this and arrives at a cleaner architecture by different means - from the amiga's explicit multi-agent model and distributed systems theory rather than from academic OS research.

this document maps barrelfish's components onto what a moxie OS would look like, identifying what to keep, what to discard, and what emerges from the moxie model that barrelfish never had.


## layer 0: the per-core executive (replaces barrelfish's CPU driver)

barrelfish runs a "CPU driver" per core: single-threaded, non-preemptible, interrupts disabled, shares no state with other cores. it schedules dispatchers, handles syscalls, manages capabilities, and processes interrupts. it uses a single statically allocated stack that resets after each handler.

moxie's version is simpler. each core runs a single executive that:

- owns all physical memory assigned to that core at boot
- runs exactly one domain at a time (the "current domain")
- handles hardware interrupts as channel messages to the appropriate domain
- performs domain switching by saving/restoring register state and page tables
- has no scheduler in the traditional sense - domains yield explicitly when their channel receive blocks

the executive is the only code that runs in privileged mode. it is not a kernel - it provides no services, no system calls in the unix sense. it provides three primitives: spawn a domain, switch to a domain, and forward an interrupt.

barrelfish's CPU driver maintains a capability database and processes capability invocations as system calls. the moxie executive does not do this. capabilities are replaced by the simpler mechanism of memory ownership - each domain owns its memory regions, and the executive enforces isolation through page tables. there is no capability transfer protocol because domains don't share memory.


## layer 1: domains (replaces barrelfish's dispatchers)

barrelfish's dispatcher is a scheduler activation - the kernel upcalls into it, and it manages its own threads. multiple dispatchers can form a "domain" that spans cores. dispatchers don't migrate between cores.

moxie domains are simpler and more constrained:

- one thread of execution, period. no internal thread scheduling.
- one domain per core is active at any time.
- a domain does not span cores. cross-core work is done by spawning a new domain on the target core and communicating via channels.
- a domain owns its memory, its channel endpoints, and nothing else.
- when a domain's channel receive has no data and all pending work is complete, it yields to the executive, which picks the next runnable domain.

the "scheduling" is implicit: a domain runs until it blocks on a channel receive. the executive then checks which other domains on the same core have pending messages and activates one. if no domain has pending messages, the core idles (or migrates a domain from a busy core - see layer 4).

this eliminates barrelfish's entire dispatcher machinery: upcall entry points (run, lrpc, pagefault, trap), enabled/disabled modes, save areas, thread control blocks, the libbarrelfish thread scheduler. none of it exists. a domain is a single execution context with a single stack.


## layer 2: channels (replaces barrelfish's IDC)

barrelfish has multiple inter-domain communication mechanisms:

- LMP (lightweight message passing) for same-core, kernel-mediated via endpoints
- UMP (user-level message passing) for cross-core, using shared memory cache-line ping-pong
- flounder, an IDL compiler that generates stubs for multiple transport backends
- waitsets (analogous to select/epoll)
- a binding/export protocol mediated by monitors

moxie replaces all of this with one mechanism: the channel pair.

a channel is a unidirectional buffer. every inter-domain connection consists of two channels - one in each direction. each channel is a fixed-size ring buffer in memory owned by the receiving domain. the sender writes to it (the executive maps a write-only view into the sender's address space). the receiver reads from it.

same-core channels: the buffer is in the receiver's memory. the sender writes, then signals the executive (a single instruction - a "notify" trap). the executive marks the receiver as runnable. no cache coherence traffic beyond the write itself.

cross-core channels: the buffer is still in the receiver's memory on the receiver's core. the sender writes via the executive, which performs the cross-core write (or uses a per-core notification FIFO like barrelfish's PCN pages). this is equivalent to barrelfish's UMP but without the polling overhead - the receiving executive gets an IPI or checks its notification slots on domain switch.

there is no IDL compiler. channel messages are typed at the language level - moxie's type system enforces the message format at compile time. the channel declaration in source code specifies the message type, and the compiler generates the serialization inline. no stubs, no runtime dispatch tables.

there is no binding/export protocol. a domain that spawns another domain provides the channel endpoints at spawn time. there is no name service, no iref lookup. you know who you're talking to because you created the connection.

backpressure: the channel buffer has a fixed size. if it's full, the sender's write blocks (yields to the executive). the receiver will eventually consume messages and free space. this is the water element - backpressure by buffer state, neither side can freeze the other, because blocking the sender just means the executive runs other domains until the receiver drains.


## layer 3: memory (replaces barrelfish's capability system)

barrelfish has an elaborate seL4-derived capability system: typed capabilities, a derivation hierarchy (RAM -> Frame -> CNode -> VNode -> Dispatcher -> Endpoint), a distributed mapping database for tracking capabilities across cores, retype/copy/mint/delete operations, and a two-level CNode addressing scheme.

moxie discards all of this. the memory model is:

- at boot, the executive partitions physical memory among initial domains. each region is owned by exactly one domain.
- a domain can subdivide its memory and give a subregion to a domain it spawns. ownership transfers completely - the parent no longer has access.
- memory cannot be shared. there is no shared mapping. inter-domain data transfer happens through channel messages (copy semantics) or bulk transfer (ownership transfer of a memory region).
- when a domain dies, its memory returns to its parent (the domain that spawned it).

this eliminates:

- the entire CNode/CSpace machinery
- the capability type system (Hamlet DSL)
- the distributed mapping database
- cross-core capability transfer protocols
- the monitor's role in mediating capability operations
- the memory server process

the executive maintains a simple list of (physical base, size, owning domain) tuples. page table construction is done by the executive on behalf of the domain - the domain requests virtual mappings of its own physical memory, and the executive constructs the page tables. domains cannot manipulate page tables directly (unlike barrelfish's self-paging model) because that would require exposing page table memory as a typed capability, which we don't have.

for bulk data transfer (network packets, disk blocks, framebuffer regions), a domain can transfer ownership of a memory region to another domain through the executive. the region is unmapped from the sender and mapped into the receiver. zero copy, no sharing, clean ownership transfer. this replaces barrelfish's bulk transfer mechanism (which relied on shared memory pools with a master-slave protocol).


## layer 4: core migration and load balancing

barrelfish dispatchers don't migrate between cores. moxie domains can.

when a core is idle and another core is overloaded (multiple domains with pending messages), the idle core's executive can request a domain migration. the migrating domain's memory is either:

- physically moved (memcpy, expensive but simple) if the domain is small
- remapped via NUMA-aware page table updates if the hardware supports it
- left in place with remote memory access accepted as a latency cost

migration is the executive's decision, not the domain's. the domain doesn't know it moved. its channels continue to work - the channel buffers get remapped, and the notification path updates to the new core.

this is simpler than barrelfish's approach because there's no dispatcher state to synchronize, no cspace to transfer, no monitor involvement. a domain is just registers + page tables + channel endpoints. all of those can be serialized and moved.


## layer 5: device drivers

barrelfish puts device drivers in user-space domains that communicate with the hardware via IO capabilities and with other domains via IDC. the driver model involves PCI enumeration, interrupt routing through the kernel, DMA buffer management, and device register access via mackerel-generated accessor code.

moxie's driver model:

- a device driver is a domain, spawned by the executive or by an init domain.
- the driver domain owns the device's MMIO region (physical memory assigned at spawn time).
- interrupts from the device are delivered as messages on a dedicated channel from the executive to the driver domain.
- DMA buffers are memory regions owned by the driver domain. when an application domain wants to do IO, it transfers ownership of a buffer to the driver domain, the driver programs the DMA, and when complete, transfers the buffer back (or to a destination domain).
- there is no PCI subsystem as a separate service. device enumeration happens once at boot, the executive reads the topology, and spawns driver domains with the appropriate memory regions.

the driver sees a flat memory region (the device registers) and a channel (interrupts + requests from other domains). it doesn't know or care about PCI, ACPI, or bus topology. the executive abstracted all of that at spawn time.


## layer 6: the knowledge base (replaces barrelfish's SKB)

barrelfish has a "system knowledge base" (SKB) that stores hardware topology information using ECLiPSe CLP (constraint logic programming). it answers queries about NUMA distances, cache hierarchies, interrupt routing, and device capabilities.

moxie replaces this with iskra.

the iskra lattice stores the hardware topology as a graph: cores, caches, memory controllers, interconnects, devices. the lattice relationships encode the physical distances and capabilities. when the executive needs to make a placement decision (which core to spawn a domain on, whether to migrate, which memory region to allocate for a device), it queries the iskra lattice.

this is more powerful than barrelfish's SKB because:

- iskra's lazy-expanding cayley tree can represent arbitrary topology without predefined schemas
- queries are lattice traversals, not constraint satisfaction problems - deterministic and fast
- the topology can be updated at runtime (hotplug, power state changes) without rebuilding a CLP database
- the same engine that handles OS topology queries handles application-level semantic queries


## layer 7: the network stack

barrelfish has a complex network stack built on top of their IDC and bulk transfer mechanisms. moxie's approach:

- the NIC driver domain owns the NIC's MMIO and DMA regions.
- incoming packets arrive as DMA into driver-owned buffers. the driver transfers buffer ownership to the destination domain based on demultiplexing (IP/port lookup).
- outgoing packets: the application domain fills a buffer, transfers ownership to the NIC driver, which programs the DMA.
- the protocol stack (TCP/IP) runs as a separate domain or as a library linked into the application domain. either way, it communicates with the NIC driver via channels.

there is no kernel network stack. the executive has zero knowledge of networking. the protocol stack is a domain like any other, subject to the same isolation and communication rules.


## what barrelfish had that moxie doesn't need

- monitors: per-core system processes that mediate cross-core capability operations, relay messages, and coordinate boot. moxie doesn't need them because there are no cross-core capabilities and channels are direct.
- the flounder IDL compiler: replaced by moxie's type system.
- waitsets/select/epoll: a domain processes one channel at a time. if it needs to multiplex, it spawns sub-domains, each handling one channel. the executive schedules them.
- the cspace/vspace distinction: there's only memory regions and page tables.
- self-paging: domains don't manage their own page tables.
- the name service (chips): domains know their communication partners at spawn time.
- LRPC/L4 RPC semantics: everything is async message passing.
- the capability type hierarchy: replaced by "you own memory or you don't."

## what emerges that barrelfish never had

- true CSP semantics at the OS level: deadlock-freedom by construction if the channel graph is acyclic.
- compile-time channel type checking: the message format is verified before the code runs.
- clean ownership transfer as the sole data sharing mechanism: no shared memory, no races, no locking, no cache coherence protocol dependency.
- the executive can be specialized per-core without any coordination: one core runs a GPU executive, another runs an ARM executive, and they communicate via the same channel mechanism.
- the iskra lattice as a unified topology + knowledge engine: barrelfish's SKB was a separate CLP system bolted on. iskra is the substrate.
- no runtime memory allocation in the executive: all memory is partitioned at boot and owned by domains. the executive's own data structures are statically sized.


## the bootstrap sequence

1. BIOS/UEFI loads the BSP (boot strap processor) executive.
2. BSP executive reads hardware topology (ACPI/device tree), builds the iskra lattice.
3. BSP executive partitions physical memory: one region per core executive, remainder for domains.
4. BSP executive boots AP (application processor) cores by loading their executives.
5. BSP executive spawns the init domain, which spawns driver domains (disk, NIC, console).
6. driver domains initialize hardware and signal readiness via channels to init.
7. init spawns the user shell or application domains.
8. all further coordination is via channels. no core has special status after boot.

step 8 is the key divergence from barrelfish, which maintains a privileged monitor on each core throughout runtime. in moxie, after boot, all cores are peers. the BSP executive has no special authority.


## implementation path

phase 1 - executive on linux: the executive runs as a linux process, "cores" are threads, domains are green threads within the process. channels are in-process ring buffers. this validates the channel semantics, domain lifecycle, and scheduling logic without touching hardware.

phase 2 - executive on bare metal (single core): boot on RISC-V or x86-64 via UEFI. single core, multiple domains, real page tables, real interrupt handling. validates memory ownership model and driver architecture.

phase 3 - multicore: boot AP cores, implement cross-core channels via shared cache-line notification pages (similar to barrelfish's PCN). implement domain migration.

phase 4 - iskra integration: replace static topology tables with iskra lattice queries for placement and migration decisions.

phase 5 - self-hosting: the moxie compiler runs as a domain on the moxie OS, compiling moxie code into moxie executables that run as domains. the bootstrap loop closes at the OS level.
