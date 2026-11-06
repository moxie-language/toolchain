# The slice

A fixed graph with a known answer, run as synchronous rounds across a bundle of
domains, to measure the costs the chip design depends on. It is not a demo: the
number is the deliverable, and a wrong number is a finding.

**The graph.** A repetition code's chain: `nodes` variable domains, one edge to
each neighbour, one channel per edge (both ends use it). Belief propagation on a
chain is exact because the chain is a tree; the received word has three flipped
bits, so the answer is the all-zero codeword.

**The rounds.** The manager (root) spawns one domain per variable node, then per
round sends every node a go, waits for every node's done, and only then starts
the next round. Each node sends its extrinsic belief on both incident edges,
receives the peer's, updates its posterior, and reports. The manager also runs
the same BP serially as the reference; the distributed decode must equal it and
the all-zero codeword, or the program dies.

**What is measured.**

- edges/s and edges/s/domain, and messages/s, over the round loop only;
- the frame price: element bytes, frame bytes (4-byte length + payload, aligned);
- allocation churn: the manager's and every node's runtime `Allocs` delta over
  the rounds, per round;
- the stall from one slow neighbour (`slow_node` busy-waits 200 us before its
  done), against the baseline;
- `waitbench`: one worker parked in `select`, one round trip through the same
  channel machinery - the wake cost per message, which is the floor under
  everything above.

Run `./run.sh`; it prints the host's model name and clock first.

## Measured (linux/amd64, 2026-11-06)

    slice nodes=32 rounds=32 edges=1984 msgs=4032
    baseline_us=20866055 edges_per_sec=95 edges_per_sec_domain=2
    msgs_per_sec=193 frame_bytes=8 element_bytes=4
    allocs_total=14589 allocs_per_round=455 bytes_per_round_est=29120
    slow_node=13 slow_us=200 slow_round_us=20671775 stall_us=-194280
    decode ok: distributed == reference, all-zero codeword

    waitbench: roundtrips=2000 total_us=14511265 us_per_rt=7255

The round loop is **wait-bound, not machine-bound**: 4096 messages in 20.9 s is
one message per ~5 ms, and the wake benchmark says one parked-select round trip
is **7.25 ms**. At that price a round of 32 nodes costs what the code says
(32 waits), and the edge and byte counts are not yet the constraint - 8 bytes
per frame and 4 bytes per element are three orders below the time cost. The
slow-neighbour stall is lost in the same noise.

So the first number the chip needs is not edges/s; it is the wake latency, and
it must come down by three or four orders before edges/s says anything about a
4x4 node at 50 MHz. Until then, edges/s/domain measures the park path, not the
silicon.
