# The transputer target

Status: provisional requirements. This document records the hardware model the
language is being aimed at, the numbers that constrain it, and what is still
undecided. Nothing here is built. The arithmetic is included so the numbers can
be checked rather than trusted.

The software kernel that runs on this machine is described in
`moxie-kernel-architecture.md`. This document is the machine underneath it.

---

## 1. What the machine is

A many-core part with no shared address space, no cache, no coherence protocol,
no speculation, and no hardware prefetcher. Each core has a private scratchpad
and one port. The only way out of a core is a message. Memory beyond the
scratchpad lives in separate memory units, reached through the port and
validated by a controller.

The simplest statement of the design: every mechanism that exists so a
programmer can pretend a distributed machine is a single shared memory has been
removed, and the cost of the removal is paid by the compiler and the programmer
instead of by silicon.

---

## 2. The node

Four to eight cores per die, one port controller per core, one on-die router.

| item | value | note |
| --- | --- | --- |
| process node | 130nm | chosen for hardening options, mask cost and SRAM density |
| die | 4x4mm to 4.5x4.5mm | 16 to 20 mm squared |
| cores | 4 at 128KB, or 8 at 64KB | see the SRAM budget below |
| scratchpad | 128KB per core | private, SRAM, not cache |
| core ISA | in-order, no speculation, no OoO | two-stage is sufficient |
| ports | 5 external | 4 planar, 1 vertical |
| endpoint address | 8 bits | 256 endpoints, cores and controllers |

### SRAM budget

A 130nm 6T bitcell is 2.0 to 3.0 square microns depending on the PDK. At 2.5:

- 64KB is 524,288 bits, 1.31 mm squared
- 128KB is 1,048,576 bits, 2.62 mm squared
- 512KB is 4,194,304 bits, 10.5 mm squared

A 4x4mm die is 16 mm squared. The pad ring takes about 3, because a 4mm edge
holds roughly 30 usable pads at 130nm and the ring is 150 to 200 microns deep.
Logic is cheap: a small in-order core is about 25k gates, roughly 0.2 mm
squared, so four cores plus a router is under 2. That leaves about 10 mm
squared for SRAM, which is 512KB total.

So the choice is 4 cores at 128KB or 8 at 64KB, and the choice is decided by
the atom measurement in section 6, not by preference.

### Pads

Five bidirectional links at two differential pairs each is 20 pads. Power,
ground, clock, reset, debug and the host interface fit in the remaining 110.
The die is pad-limited at 4x4mm, which is another argument against many small
cores.

---

## 3. The interconnect

Two networks, deliberately different.

**Inter-chip: serial mesh.** Four planar links plus one through-board link, so
each chip connects to its four neighbours and to the chip on the opposite face.
500 Mbit/s to 1 Gbit/s per link is achievable with plain LVDS at 130nm. On an
8x8 grid per layer the bisection is four links, so about 250 MB/s at 500 Mbit/s.
Aggregate per chip is roughly 2.5 Gbit/s across five links.

Routing is dimension-order, X then Y then Z. Z is a single hop, so a packet
that goes up never needs to come down, which removes the usual 3D deadlock
cases. Wormhole switching with credit-based flow control. The mesh tolerates
any single link failure; corners are the weak point because they have degree
two or three.

**On-die: wide parallel.** A 32-bit flit at 100 MHz is 3.2 Gbit/s, a 64-bit
flit at 200 MHz is 12.8 Gbit/s. This is 20 to 80 times the serial link and it
is what makes on-die loads affordable. The two networks meet at the chip edge
and must not share a protocol; the serial framing exists to make cables cheap
and has no place inside the die.

**Addressing.** The 8-bit space is an endpoint space, not a processor space. It
contains cores, memory controllers and the host interface. Decide this early,
because the routing table and the memory allocation table are both indexed by
it.

---

## 4. The memory model

Memory is a named extent store, not a flat array and not a block device. The
term "ramdisk" is misleading because it implies sector granularity, which costs
4KB for an 8-byte access. The interface is open, map, unmap, close.

### The memory bus

Two requirements pull against each other: the bus must be fast enough that a
64KB load fits inside a task, and it must be arbitrated so no core can starve
another.

**Split-phase transactions.** The address phase and the data phase are
separate. A master issues a request and releases the bus immediately, and the
response returns later, tagged, routed back to the requester. The conventional
pipelined bus already does this in one direction, but making the release
explicit means the bus is not held during memory latency, which is what allows
several requests to be outstanding at once. The cost is an identifier per
outstanding transaction, so each requester needs a small table of in-flight
slots. Four masters at a pipeline depth of four is sixteen identifiers.

**Time-slot arbitration at twice the core clock.** Run the memory at 2x the
core clock and give each core a fixed slot in a rotation. This is the Amiga's
odd/even interleave generalized past two masters. Fixed slots make the
worst-case service interval computable rather than statistical: with N masters
and two slots per core cycle, each master gets one transfer every N/2 core
cycles. That hard bound is what the loader needs to decide whether a load fits
inside a task, and it is the reason to prefer fixed slots over round-robin
with priority, which gives a better average and no bound.

Numbers. Core at 100 MHz, memory at 200 MHz, two slots per core cycle, 16-byte
transfers:

| masters per bank | slots per master per core cycle | per master | 64KB load |
| --- | --- | --- | --- |
| 1 | 2 | 3.2 GB/s | 20 us |
| 2 | 1 | 1.6 GB/s | 41 us |
| 4 | 0.5 | 800 MB/s | 82 us |
| 8 | 0.25 | 400 MB/s | 164 us |

So masters per bank is the lever, and it is a stronger lever than bus width at
these counts. Four masters sharing a bank already puts a 64KB load at 82 us,
which exceeds a 40 us task. The ways out are 256-bit transfers, two banks per
core, or fewer cores per bank, and all three are cheaper to decide on paper
than to discover after tapeout.

**Isolation is physical.** A core's address decoder decodes only its own
scratchpad. The core cannot form an address for another core's memory, so the
guarantee is stronger than an MMU check: the request cannot be expressed. The
only path out is the port.

**The controller is the enforcement point.** A message names a destination
extent. The memory controller validates the source endpoint against the
allocation table and refuses anything outside it. So the precise claim is: a
core cannot address memory it does not own, and the only way out is a message
that the receiver checks. That is a check, not an absence, and the difference
decides what has to be verified.

**The allocation table is static for the life of the boot.** If the kernel can
rewrite it at runtime, the kernel becomes the attack surface and the partition
becomes a policy rather than a guarantee. This is cheap now and expensive
later.

**Banked, not crossbarred.** Bank the memory so each bank serves a small set of
masters and arbitrate per bank, which is what makes the table above a design
input rather than an outcome: the bank count chooses the row. A full crossbar
from eight cores to eight banks is 64 arbitration points and the wiring
dominates the logic. Four banks with two masters each gives the 41 us row; two
banks with four masters each gives the 82 us row.

**Two levels.** Scratchpad for the working set, backing store for everything
else. The backing store is large and slow and lives in the edge memory units.
It is bulk storage, not memory: the mesh cannot deliver it fast enough to be
treated otherwise.

---

## 5. Scheduling

**The rest point is the arena pop at function return.** The language already
defines it, and it is the same event as the memory reclaim. This fixes the atom
granularity: the atom cannot be smaller than a function, because there is
nowhere to stop inside one.

**The atom is the select arm, not the loop.** A loop containing a select runs
many times; if the loop is the atom, every arm must be resident and there is
nothing to swap. Choosing the arm makes the loop the scheduler and the select
the scheduling point.

**Cooperative, with a watchdog.** A function that does not return never yields.
For a compiler that is acceptable, because well-formed code has no infinite
loops, but a bug becomes a hang rather than a preemption. The watchdog is in
the kernel from the start.

**Migration is rare.** Moving a live arena is a bulk transfer: 64KB at 4 Gbit/s
is about 128 microseconds. Migration only pays if the task will run much longer
than that, and the scheduler does not know task duration. So migration is a
heuristic for long tasks, not a primitive. Prefetch is the primitive, because a
wrong prefetch costs a wasted load and a wrong migration costs a stall.

---

## 6. The dispatcher

This replaces the branch predictor and the hardware prefetcher, and the
replacement is exact rather than statistical.

A message arriving on a channel is a statement that work exists, addressed to
the arm that will handle it. A branch predictor guesses what you will need; a
dispatcher is told. That is the whole architectural claim: prediction becomes
declaration, and a declaration cannot be wrong, only late.

**The load protocol.**

1. The port controller receives a message and looks up its type in a static
   table mapping message type to code address and slot size.
2. It starts a DMA into the spare scratchpad buffer, without involving the
   core, which is busy running the current arm.
3. The core continues. When the current arm finishes, the select finds the arm
   ready, the buffers swap, and the new code runs with the message payload as
   its parameter.

**The sizes are compile-time constants.** The per-arm residency figure is a
static property emitted into the symbol table, because the loader has to
reserve the slot before the message arrives. If the size is only known at
runtime, the scheduler cannot reserve and the arrangement collapses to a
stall-and-wait. This is A1 applied one level down: declared, not discovered.

**The overlap condition.** Load time must be less than or equal to task
duration. That is the design rule, and it replaces "load in a few cycles",
which is not achievable at any bus width.

**Arms must be leaves.** If an arm calls into a library, the transitive closure
is the real working set and it will not fit. Arms are small handlers, not entry
points into subtrees. This is a constraint on how domains are written, not on
the hardware.

**Code is local.** A 4KB atom over a 100 Mbit link is 327 microseconds, which
is eight times a 40 microsecond task. Code fetched across the mesh is
unusable, so each core carries its own copy of what it runs and the mesh
carries only messages. This is the cost of no shared memory, and it is why the
serial network is for data and triggers only.

---

## 7. Numbers

Load times for a 64KB buffer, which is half a 128KB scratchpad. Each cycle
moves bus-width divided by 8 bytes.

| bus | bytes/cycle | cycles | at 100 MHz | at 200 MHz |
| --- | --- | --- | --- | --- |
| 128-bit | 16 | 4096 | 41 us | 20 us |
| 256-bit | 32 | 2048 | 20 us | 10 us |
| 512-bit | 64 | 1024 | 10 us | 5 us |
| 1024-bit | 128 | 512 | 5 us | 2.6 us |

Four cycles would need 16KB per cycle, which at 200 MHz is 3.3 TB/s. There is
no such bus. "Four cycles" is the cache-line figure: a 64-byte line over a
128-bit bus is four cycles, and the number has been applied to 1024 lines.

Bandwidth per core, and why atom size decides feasibility:

- 4KB atoms with 40 microsecond tasks: 100 MB/s per core, 6.4 GB/s across 64
  cores. A few 128-bit buses. Buildable at 130nm.
- 64KB atoms with the same task duration: 1.6 GB/s per core, 102 GB/s across
  64 cores. That is 8192 bits of bus. Not buildable.

So the atom size, not the bus width, decides whether the memory system is
feasible.

Serial link reference:

- 4KB over 100 Mbit/s: 327 us
- 4KB over 1 Gbit/s: 33 us
- 64KB over 100 Mbit/s: 5.2 ms

---

## 8. The trusted computing base

The kernel, the memory controllers, and the port controllers. Everything else
is application.

This is smaller than it sounds, and the reason matters. The Amiga's TCB was the
whole OS plus a CPU with no protection, so any bug anywhere was total. Here the
enforcement is in a comparator and a lookup table: a source endpoint check
against a static allocation table, and a message type check against a static
type table. Both are a few hundred gates. Neither is an interpreter, a
scheduler, or a page table walker.

The honest statement is that the TCB is small and distributed enough to verify,
not that there is no centre. Claiming no centre is falsified by the first
controller validation bug anyone finds.

**Policy in software, mechanism in silicon.** A cache line is cheap in silicon
and expensive in software, so the mechanism stays. Replacement policy, prefetch
policy, migration policy and coherence policy are the things that might need to
change, so those move. That is the same separation as microcode against
hardwired control, and it is why RISC won on flexibility rather than speed.

The limit on the survival claim: software can revise policy, it cannot revise
mechanism. If a vulnerability is in the ISA itself, the same silicon does not
survive. The defensible claim is "policy is revisable without new silicon".

---

## 9. Open decisions

1. **Atom size.** Undecided, and it is decided by measurement, not argument.
   The study is: build the call graph from the compiler's own `.ll` output,
   take each select arm and spawn target as a root, sum the closure's symbol
   sizes from `nm --size-sort`, and produce the distribution. The metric is the
   distribution, not the mean. Expectation is bimodal: many small handlers and
   a few monsters. If the middle is fat, 128KB is the wrong size and it is
   better to learn that before tapeout.
2. **Four cores at 128KB or eight at 64KB.** Falls out of open decision 1.
3. **Stack machine or register machine.** A stack machine needs no register
   allocator, so the compiler is smaller and the generated code is larger. A
   register machine needs linear scan at minimum. This changes the cost class
   of every operation, so it must be decided before the language cost table is
   written.
4. **Vertical link or not.** A four-port first die is a much smaller thing to
   debug and loses cross-layer links on the prototype. Adding Z in a second
   spin is cheaper than debugging two new mechanisms at once.
5. **Message frame size.** Fixed and allocated at boot, enforced at the
   receiver. The size is open; 64 and 256 bytes are the candidates. A receiver
   that allocates from a header field can be told to allocate a gigabyte.
6. **Endpoint address allocation.** How the 8-bit space divides between cores,
   memory controllers and the host.
7. **Backing store size.** Not a hardware question in the end: size it to the
   largest task's working set plus code, and treat it as bulk storage.
8. **Inbound codec.** Whether messages carry operands inline or as handles.
   Inline keeps the atom small and avoids a second fetch; a handle means two
   fetches and double the bandwidth.
9. **Arbitration scheme.** Fixed time slots give a computable worst case, which
   is what the loader needs. Weighted round-robin gives a better average and no
   bound. Priority with ageing starves. The choice is really whether the
   loader's decision is made against a guarantee or a measurement.
10. **Outstanding transaction depth.** Split-phase needs an identifier per
    in-flight request. The depth sets the table size at each master and the
    buffer count at each bank, and it bounds how much latency can be hidden.

---

## 10. What would falsify this

The design survives if the atom measurement lands mostly under 16KB and the
pipeline balances. It fails if:

- the closure sizes in real code are dominated by values above 32KB, which
  makes 128KB scratchpad too small and the scheduling rest point too coarse;
- a balanced pipeline cannot be built because stages have wildly different
  service rates, which makes the message-arrival prefetch unreliable in steady
  state;
- the memory controller validation turns out to need a runtime table, which
  moves the kernel into the security boundary in a way that stops being
  verifiable.

Each of those is checkable before silicon, which is the point of writing them
down.
