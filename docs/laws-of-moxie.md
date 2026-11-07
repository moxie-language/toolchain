# Laws of Moxie

Operational methodology for building coherent systems. Each law addresses a failure mode observed in practice — not a theoretical concern, but a pattern that has destroyed weeks of work when violated.

---

## 1. Bilateral Contracts

**Problem.** Two systems communicate through a boundary. One side changes its interface. The other side doesn't know. Nothing fails at compile time. Data flows through the boundary silently corrupted — wrong types, missing fields, calls into void. The developer sees symptoms downstream and chases ghosts for days because the actual break is invisible.

**Law.** Every function, message, or protocol that crosses a system boundary must be defined on both sides simultaneously. If you add a callable on one side, the implementation on the other side ships in the same change. If you rename, both sides rename together. There is no "I'll update the other side later." Later is where bugs hide. A boundary contract is not a contract if only one party signed it.

---

## 2. Tokens, Not Objects

**Problem.** A complex object — a connection, a DOM node, a file handle, a session — gets passed across a boundary. The receiving side stores a reference to something it doesn't own and can't inspect. The originating side garbage-collects, mutates, or invalidates the object. The reference becomes a landmine. Now you have use-after-free semantics in a language that supposedly doesn't have pointers.

**Law.** Boundaries communicate via opaque tokens — integers, strings, UUIDs. The token is a key into a lookup table owned by the side that created the object. The other side never touches the object directly. It sends the token back when it wants something done. This is not overhead. This is the only pattern that survives process restarts, serialization, and the passage of time.

---

## 3. Async Resolves at the Boundary

**Problem.** An async operation starts on one side of a boundary and returns a promise or future to the other side. Now the other side holds a live reference to an execution context it doesn't control. Cancellation semantics diverge. Error propagation diverges. The promise might resolve after the receiver has moved on, or never resolve at all. The two sides are now coupled by shared mutable time.

**Law.** Async work completes into a callback at the boundary. The initiator fires the request and forgets. The executor does the work and calls back with the result. The callback is called exactly once — success or failure. If the executor might hang, the initiator sets its own timeout. Neither side holds a live reference to the other's execution state. Promises, futures, and channels do not cross boundaries.

---

## 4. Serialize at the Boundary

**Problem.** A structured object — a map, a struct, a nested array — crosses a boundary as a native object. The receiving side assumes the shape. The sending side changes the shape. Or the serialization is implicit and adds fields the receiver doesn't expect. Or the object contains types that don't survive the crossing — functions, circular references, platform-specific wrappers. The data arrives and looks right until it doesn't.

**Law.** Every boundary is an explicit serialization point. Data crosses as strings (JSON, msgpack, protobuf — pick one). The sender serializes. The receiver deserializes. No native objects cross. No implicit coercion. If you're looking at a boundary and you don't see a serialization step, the boundary is lying to you about being a boundary.

---

## 5. Shape Encodes Intent

**Problem.** Messages flow through a system and need to be routed to different handlers. A metadata field says "type: X" but the parser has to deserialize the whole message to read it. Or the type field is missing. Or it conflicts with the payload. Or two subsystems use the same type field name for different purposes. Routing becomes fragile and expensive.

**Law.** The shape of a message — its outermost structural character, its prefix, its envelope — determines where it goes. An array is an application message. An object is a bus envelope. A string starting with a known prefix routes to a specific handler. The router never needs to parse the payload. It inspects the surface and forwards. If you can't route a message by looking at its first byte, your protocol has a design flaw.

---

## 6. Peers Are Absent Until Proven Present

**Problem.** System A sends a message to System B. System B hasn't started yet, or has restarted, or is temporarily suspended by the runtime. The message vanishes. System A doesn't know. It assumes delivery and builds state on a foundation that doesn't exist. Everything downstream is wrong but nothing throws an error.

**Law.** Every peer-to-peer channel implements a readiness handshake. Messages sent before the peer confirms readiness are queued with a bounded buffer. When the buffer fills, the oldest messages are dropped — not the newest, and not the system. The peer announces readiness with a version. If versions mismatch, the connection is re-established, not patched. Assume every peer is absent. Be pleasantly surprised when it's not.

---

## 7. Secrets Traverse the Shortest Path

**Problem.** A cryptographic operation needs a private key. The key exists in Component A. The operation needs to happen for Component C. The developer routes the key through Component B because B sits between them in the architecture. Now B has seen the key. B's logs have seen the key. B's error handlers have seen the key. The attack surface just tripled for no functional reason.

**Law.** Sensitive material — keys, tokens, credentials, plaintext — travels through the minimum number of boundaries required to reach its destination. If a proxy is needed, the proxy forwards opaque requests, never the material itself. Every intermediate node that sees a secret is a liability. Crypto operations happen where the keys already live. Everything else sends a request and receives a result.

---

## 8. Know What Your Tools Cost

**Problem.** A developer imports a serialization library. It works. The binary size doubles. Or startup time triples. Or it pulls in a reflection system that breaks the target runtime. The developer didn't know because the cost isn't in the API signature — it's in the dependency graph, the binary output, the runtime initialization. The tool solved a five-line problem and created a five-hundred-kilobyte problem.

**Law.** Before using any tool, library, or language feature, know its cost in binary size, startup time, runtime dependencies, and compatibility constraints. If the cost exceeds the value, implement the functionality directly. String concatenation that builds JSON is ugly. A reflection-based marshaler that doubles your binary size is uglier. Cost is not subjective — measure it. "Everyone uses this" is not a measurement.

---

## 9. Generated and Authored Code Have Different Owners

**Problem.** A build tool generates output files. A developer hand-edits one of them to fix a bug. The build tool runs again. The fix is gone. Or worse: the developer doesn't know which files are generated and which are authored. They edit the wrong one. It works until the next build. Then it silently reverts and the bug reappears from nowhere.

**Law.** Generated code and authored code must be distinguishable by location, naming convention, or both. Generated code is never edited by hand — if it's wrong, fix the generator. Authored code is never overwritten by a build step. When both coexist in the same directory, the convention must be documented and enforced. "Which files can I edit?" should never require guessing.

---

## 10. The Common Ancestor Routes for Isolated Siblings

**Problem.** Two processes need to communicate but run in isolated contexts — different origins, different threads, different sandboxes. The developer tries to create a direct channel. The runtime forbids it. So the developer routes through the server. Now a local operation has network latency, server load, and a new failure mode. The architecture has been corrupted to work around a sandbox.

**Law.** When isolated siblings need to communicate, the nearest common ancestor acts as router. The ancestor doesn't process the messages — it forwards them. The routing code runs in the ancestor's context, not in application logic, because it must be active before application code loads. The server is never the common ancestor for client-side processes. If two things run in the same browser, their common ancestor is the page, not the cloud.

---

## 11. Naming Is Routing When You Can't Add Metadata

**Problem.** A system handles messages from multiple sources through a single channel. Some messages need special processing. The developer adds a metadata field. But the channel is a third-party protocol that doesn't support metadata. Or the receiver can't inspect metadata without full deserialization. The special messages are indistinguishable from regular ones until it's too late.

**Law.** When a protocol or channel doesn't support explicit routing metadata, encode routing intent in the names you control — subscription IDs, topic prefixes, queue names. A prefix convention is not a hack. It's routing for systems that can't afford a routing layer. The prefix is checked before deserialization. If it matches, the message goes to the special handler. If not, it flows through the default path. The cost is one string comparison. The alternative is a new protocol layer.

---

## 12. Boundaries Absorb Errors

**Problem.** An exception is thrown on one side of a boundary. It propagates to the other side. The other side doesn't understand the exception type, the stack trace, or the context. It either crashes, swallows the error silently, or wraps it in a generic error that destroys all diagnostic information. Exception propagation across boundaries is a fiction — the semantics never survive the crossing.

**Law.** Errors do not propagate across boundaries. A function that fails at a boundary returns a zero value, an error code, or calls the error callback. The caller checks the return. The exception stays where it was thrown, logged in the context that can diagnose it. The boundary translates failure into a value the other side can act on — not a stack trace it can't read, and not an exception it can't catch.

---

## 13. Every Dependency Compounds

**Problem.** A developer adds a library for one function. That library imports three others. Those import five more. The dependency tree is now twelve packages deep. One of them has a security vulnerability. One is unmaintained. One conflicts with the target runtime. The developer used one function. The project inherited twelve liabilities.

**Law.** Every dependency has a cost that compounds — in security surface, maintenance burden, build time, binary size, and runtime compatibility. The cost is not the dependency itself but its transitive closure. Before adding any dependency, implement the needed functionality from scratch. If it takes fewer than fifty lines, it takes fewer lines than reading the library's documentation. If it takes more, evaluate whether the library's transitive cost is worth the saved lines. The answer is usually no. The project that depends on nothing breaks only when you break it.

---

## Coda

These laws are not preferences. They are observed invariants — patterns that, when violated, produce silent corruption, week-long debugging sessions, and systems that appear to work until they don't. They were extracted from a codebase where every violation was eventually paid for in full.

The common thread: **boundaries are where coherence dies.** Every law is about maintaining coherence across a boundary — between systems, between processes, between generated and authored code, between what you control and what you don't. Master the boundaries and the interiors take care of themselves.
