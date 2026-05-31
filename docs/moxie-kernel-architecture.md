# moxie kernel architecture

## startup sequence

the bootloader (GRUB multiboot or equivalent) places three blobs into RAM: the compressed kernel core, the boot bundle (block + filesystem drivers), and their memory addresses. the bootstrap stub runs first - switches CPU modes, decompresses the kernel core, builds initial page tables, enables the MMU, and jumps to the kernel entry point.

the kernel core initializes its subsystems, then spawns the boot bundle drivers from the pre-loaded images. once block and filesystem drivers are running and connected via channels, the system can load further modules from disk.

## kernel core (executive)

the minimum resident image. nothing outside this blob runs without it.

MMU management handles page tables, physical frame allocation, virtual address spaces, and page faults. the scheduler owns context switching, time accounting, and thread lifecycle. trap dispatch sets up the interrupt vector, routes CPU exceptions and syscalls, and manages ring transitions. the channel runtime is the IPC substrate - create, destroy, send, receive, buffer management - and expresses backpressure through buffer state, never by blocking the sender. the ELF loader resolves symbols against the kernel's export table and maps relocatable objects into memory.

the channel runtime is the foundation everything else communicates through. it cannot depend on any loadable module.

## boot bundle

loaded into RAM by the bootloader alongside the kernel - not from disk, because disk access doesn't exist yet.

the block driver talks to storage hardware (NVMe, SATA, virtio-blk). the filesystem driver sits on top of it, providing path-based access. both are spawned by the kernel core and communicate exclusively through channels - with the kernel and with each other. once they're running, the ELF loader can fetch further modules from storage.

the block driver is configured at boot (via bootloader command line or device tree) to know which device holds the root filesystem. the filesystem driver attaches to the block driver's channel and exposes a read/lookup interface that the ELF loader uses.

## per-thread core

every Spawn'd thread gets a small, self-contained execution context. this is the barrelfish-style per-core dispatcher shrunk down to per-thread granularity, because Spawn (not the CPU core) is moxie's unit of concurrency.

**memory arena.** a thread-local region allocator. allocation is a bump pointer into a pre-mapped region; no global heap lock, no contention. when the thread dies, the entire arena is freed in one operation. the arena is the thread's sovereign territory - earth law. the parent can set a ceiling on arena size at spawn time.

**channel handle table.** the set of channel endpoints this thread owns - its capability set. a thread can only send or receive on handles it holds. handles are passed explicitly at spawn time or transferred over an existing channel. this is metal law: the thread gets a key, not the house.

**parent channel.** every Spawn'd thread has an implicit bidirectional channel back to its spawner. thread death, panics, and fault reports travel over this channel. death is reported, not hidden or auto-recovered - earth law. if the parent dies first, the thread's parent channel becomes inert (sends succeed into a void, receives return closed).

**trap frame.** saved register state for context switching. owned by the scheduler, written on preemption or voluntary yield, restored on resume. one per thread, always.

**stack.** per-thread stack with a guard page below it. sized at spawn time, not growable (the arena handles dynamic allocation). stack overflow hits the guard page, triggers a fault, reported to parent via the parent channel.

**scheduler metadata.** time slice accounting, priority, CPU affinity. the scheduler reads this to decide what runs next. a thread cannot modify another thread's scheduler metadata - only the kernel core can, via syscall.

## scheduling

preemptive, tickless, deadline-based. no periodic timer interrupt. the scheduler programs a one-shot hardware timer for each scheduling decision.

when a thread is dispatched onto a core, the scheduler sets a one-shot timer for that thread's deadline - the maximum time it may run before the next context switch. when the timer fires, trap dispatch catches it, the scheduler saves the current trap frame, selects the next runnable thread, restores its trap frame, programs a new one-shot timer, and resumes.

if only one thread is runnable on a core, no timer is set. the thread runs until it blocks on a channel receive, yields, or another thread becomes runnable (at which point an IPI or channel-runtime event triggers rescheduling). this is the core benefit of tickless - an idle-except-for-one-thread core burns zero scheduling overhead.

the deadline quantum scales with contention. with two threads on a core, each gets longer quanta. as the runnable count rises, quanta shrink toward a floor determined by context switch cost. on modern x86 with PCID (avoiding full TLB flush on CR3 swap), context switch is roughly 1-2μs. that puts the practical floor around 100μs (10000hz) - below that, switching overhead dominates. a reasonable default range is 100μs to 1ms depending on thread count and priority.

priority is expressed as deadline tightness, not as a separate axis. a high-priority thread gets shorter deadlines (scheduled sooner, more often) rather than longer quanta. this avoids the classic priority-inversion problem where a high-priority thread with a long quantum starves everything else while it holds a resource - but in moxie there are no shared resources to hold, so priority inversion is structurally impossible anyway. priority-as-deadline-tightness is simpler and sufficient.

threads that block on channel receive before their deadline expires return their remaining time to the core immediately - the scheduler picks the next thread and programs a fresh timer. no time is wasted waiting for a tick that isn't coming.

## thread suspend and migration

because the per-thread core is self-contained with no shared state, suspend/serialize/deserialize is a structural property rather than a heroic runtime hack. compare CRIU on linux, which must introspect scattered global kernel state (fd tables, shared mappings, signal queues, socket buffers, cgroup attachments) from outside the process. here, the entire thread world is already one bundle.

### suspend

the kernel stops the thread, writes its trap frame (final register snapshot), and marks it suspended. the channel runtime marks all of the thread's channel endpoints as suspended - peers see their sends succeed into buffer (water law - backpressure, not blocking), receives from that endpoint return a "peer suspended" status. no channel is closed; the handles stay valid but dormant.

### serialize

the kernel writes the suspended thread image to a well-known filesystem path:

    /sys/suspended/<thread-id>.img

the image contains: arena contents (raw memory), channel handle table (handle IDs and their channel-runtime-side buffer references), trap frame, stack contents, scheduler metadata, and a reference to the ELF module the thread was spawned from (not the module itself - that's already on disk). the format is a flat serialization with a header, a table of sections, and the sections themselves. checksummed.

the filesystem location is the canonical intermediate form. even for local core-to-core migration, the image goes to disk first. this makes the operation durable (crash between suspend and resume doesn't lose the thread), auditable (the image is inspectable), and uniform (same path whether migrating to the next core or to another machine).

### deserialize and resume

to resume a thread (locally or on a remote machine):

1. read the image from /sys/suspended/
2. allocate a fresh arena and copy the arena contents into it
3. remap any internal pointers that reference the old arena base to the new base (the arena is position-independent by construction - all internal references are offsets from arena base, not absolute addresses)
4. restore the trap frame and stack
5. reconnect channel handles to the channel runtime - the runtime replays any messages that arrived while the thread was suspended
6. the thread enters the scheduler and resumes from exactly where it stopped

for cross-machine migration, channel endpoints that were local become network-bridged. the channel runtime on the source machine holds a forwarding entry: messages arriving for the old endpoint get shipped to the new machine's channel runtime over the network transport. this is transparent to both the migrated thread and its peers - neither knows the topology changed.

### the /sys/suspended/ directory

this directory is managed by the kernel core. threads can be enumerated, inspected, resumed, or discarded from here. a suspended thread image is a first-class filesystem object - it can be copied, backed up, or transferred to another machine via any file transport. the thread doesn't care how it got there.

a thread tree (parent + children) can be suspended as a unit. the image captures the full tree with internal channel connections preserved. resume reconstructs the tree and reconnects internal channels before reconnecting external ones.

## distributed cluster

the suspend/serialize/deserialize path is transport-agnostic. whether the thread image moves to the next core via shared memory or to another machine via 10gbit ethernet, the resume procedure is identical. this makes a cluster of same-architecture machines act as one computer from the perspective of any running thread.

### supervisor trees

a supervisor is a thread that manages a set of worker threads. it holds: the channel handle to each worker's parent channel, the filesystem path of each worker's last checkpoint image, and the machine each worker is currently running on.

when a machine dies, the supervisor receives channel-closed notifications for every worker on that machine. it reads their last checkpoint images (stored on networked/replicated storage), selects surviving machines with available capacity, and respawns the workers there. the workers resume from their last checkpoint and reconnect channels. from the worker's perspective, nothing happened - it doesn't know it died and moved.

supervisors are themselves threads with their own parent channels, forming a tree. a supervisor can supervise other supervisors. the root supervisor runs on a designated machine (or a quorum of machines for redundancy). this is Erlang's supervision model pushed down to the OS layer, where it captures full hardware state (registers, stack, arena) rather than just language-level process state.

### hot-plug scaling

a new machine boots the moxie kernel, starts its boot bundle, and announces itself to the cluster supervisor over the network. the supervisor immediately has a new target for thread placement. no reconfiguration, no rebalancing pass, no downtime. need more compute - plug a box into power and network.

the supervisor's placement decisions are simple: which machine has capacity? migrate or spawn there. a machine going offline is the reverse: checkpoint its threads, redistribute. the thread images are the universal unit of work - they move freely across any machine in the cluster.

### client attachment

clients (UI, terminal, external consumers) connect to the cluster through channel endpoints exposed over the network. the client doesn't know or care which machine its server thread is running on. if the front-end machine dies, the client reconnects to any surviving machine's network listener, the supervisor routes it to the appropriate server thread (which may have been migrated), and the session continues.

for a workstation scenario: one machine owns the display, keyboard, mouse, sound. the other machines are pure compute. the UI machine is a single point of failure for the display path, but the compute state is fully distributed - a backup UI machine can be hot-swapped in, reconnect to the same server threads, and resume the session. the compute threads never noticed.

for a data center: hundreds or thousands of machines, each running the same kernel, connected by fast network. threads migrate freely based on load. machines join and leave without disruption. the supervisor tree maintains the mapping. the cluster scales by plugging in boxes and shrinks by unplugging them.

### distributed media and compute

the channel-over-network transport handles any streaming workload. visual is the heaviest case and sets the ceiling - if the architecture handles 200,000 object transforms at 100hz on 10gbit (64 bytes per transform, ~1.2GB/s link capacity), everything else fits underneath:

- 3d/simulation: geometry and physics threads on compute nodes stream per-frame transform deltas to a front-end that caches meshes and textures locally. the front-end composites and presents. the compute threads don't know they're remote.
- audio: synthesis, mixing, and DSP threads on compute nodes stream PCM buffers to a front-end that owns the sound device. 48kHz stereo 32-bit float is 384KB/s - negligible on any network.
- video: decode, transcode, or compositing threads stream frame buffers or compressed chunks to a display front-end. even uncompressed 1080p60 (370MB/s) fits within 10gbit with room to spare.
- sensor/control: industrial or robotics threads processing sensor input and emitting actuator commands. sub-KB messages at kHz rates. the 100μs scheduling quantum guarantees hard real-time response within the cluster.
- spatial/radio mesh: sensor and actuator nodes distributed over physical space (factory floor, building, outdoor installation) connected by radio mesh (WiFi, 802.15.4, LoRa). radio propagation within 100m is sub-microsecond; mesh network latency is low single-digit milliseconds. sub-KB control messages at kHz rates fit easily within this budget. the entire space responds as a single system - threads on different physical nodes communicate over channels bridged by radio, supervised and recoverable like any other cluster node. a sensor node failing is handled identically to a data center machine failing: supervisor checkpoints, respawns on a surviving node, reconnects channels.
- scientific compute: simulation partitions run on separate machines, exchange boundary conditions via channels each timestep. same pattern as MPI but without the MPI runtime - channels are the transport natively.

in every case the application code is unaware of distribution. it sends and receives on channels. the channel runtime and supervisor tree handle placement, migration, failure recovery, and scaling. the threads are sovereign units of work that move freely across the cluster.

## what this means in practice

the full boot sequence is: bootstrap stub -> kernel core init -> spawn block driver -> spawn filesystem driver -> filesystem attaches to block driver via channel -> ELF loader can now reach disk -> load everything else.

every module after the boot bundle is a spawned thread (or tree of threads) with its own arenas, its own channel handles, and a parent channel reporting back to whatever spawned it. no module shares memory with any other. no module can block another module's execution. the channel buffer is the only shared state, and it's managed by the kernel core's channel runtime.

the per-thread core keeps each spawned unit sovereign and independently killable. a crashed driver's arena gets freed, its channel handles get closed (notifying peers), and its parent gets a death report. no global state is corrupted because no global state was shared.
