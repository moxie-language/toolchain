.globaltype __stack_pointer, i32

.functype start_unwind (i32) -> ()
.import_module start_unwind, asyncify
.import_name start_unwind, start_unwind
.functype stop_unwind () -> ()
.import_module stop_unwind, asyncify
.import_name stop_unwind, stop_unwind
.functype start_rewind (i32) -> ()
.import_module start_rewind, asyncify
.import_name start_rewind, start_rewind
.functype stop_rewind () -> ()
.import_module stop_rewind, asyncify
.import_name stop_rewind, stop_rewind

.global  moxie_unwind
.hidden  moxie_unwind
.type    moxie_unwind,@function
moxie_unwind: // func (state *stackState) unwind()
    .functype moxie_unwind (i32) -> ()
    // Check if we are rewinding.
    i32.const 0
    i32.load8_u moxie_rewinding
    if // if moxie_rewinding {
    // Stop rewinding.
    call stop_rewind
    i32.const 0
    i32.const 0
    i32.store8 moxie_rewinding // moxie_rewinding = false;
    else
    // Save the C stack pointer (destination structure pointer is in local 0).
    local.get 0
    global.get __stack_pointer
    i32.store 4 // state.csp = getCurrentStackPointer()
    // Ask asyncify to unwind.
    // When resuming, asyncify will return this function with moxie_rewinding set to true.
    local.get 0
    call start_unwind // asyncify.start_unwind(state)
    end_if
    return
    end_function

.global moxie_launch
.hidden moxie_launch
.type moxie_launch,@function
moxie_launch: // func (state *state) launch()
    .functype moxie_launch (i32) -> ()
    // Switch to the goroutine's C stack.
    global.get __stack_pointer // prev := getCurrentStackPointer()
    local.get 0
    i32.load 12
    global.set __stack_pointer // setStackPointer(state.csp)
    // Get the argument pack and entry pointer.
    local.get 0
    i32.load 4 // args := state.args
    local.get 0
    i32.load 0 // fn := state.entry
    // Launch the entry function.
    call_indirect (i32) -> () // fn(args)
    // Stop unwinding.
    call stop_unwind
    // Restore the C stack.
    global.set __stack_pointer // setStackPointer(prev)
    return
    end_function

.global  moxie_rewind
.hidden  moxie_rewind
.type    moxie_rewind,@function
moxie_rewind: // func (state *state) rewind()
    .functype moxie_rewind (i32) -> ()
    // Switch to the goroutine's C stack.
    global.get __stack_pointer // prev := getCurrentStackPointer()
    local.get 0
    i32.load 12
    global.set __stack_pointer // setStackPointer(state.csp)
    // Get the argument pack and entry pointer.
    local.get 0
    i32.load 4 // args := state.args
    local.get 0
    i32.load 0 // fn := state.entry
    // Prepare to rewind.
    i32.const 0
    i32.const 1
    i32.store8 moxie_rewinding // moxie_rewinding = true;
    local.get 0
    i32.const 8
    i32.add
    call start_rewind // asyncify.start_rewind(&state.stackState)
    // Launch the entry function.
    // This will actually rewind the call stack.
    call_indirect (i32) -> () // fn(args)
    // Stop unwinding.
    call stop_unwind
    // Restore the C stack.
    global.set __stack_pointer // setStackPointer(prev)
    return
    end_function

        .hidden moxie_rewinding                # @moxie_rewinding
        .type   moxie_rewinding,@object
        .section        .bss.moxie_rewinding,"",@
        .globl  moxie_rewinding
moxie_rewinding:
        .int8   0                               # 0x0
        .size   moxie_rewinding, 1
