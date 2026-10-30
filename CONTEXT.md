# Factorybox

Factorybox preserves complete Linux development environments across independent
branches, pauses and supported worker replacement.

## Language

**Sandbox**:
One running or saved application environment with its own process and filesystem
state. Its descendants can continue independently of their original source.

**Worker**:
One privileged administrative boundary assigned to one customer or trust domain.
Its lifetime is separate from the lifetimes of the environments it preserves.

**Worker epoch**:
One identified worker lifetime. Replacing a worker starts a new epoch without
making an old live process identity reusable.

**Persistent store**:
The owned data required to preserve and recover environments after supported
worker replacement. Deleting a sandbox does not imply retiring its stored data.

**Store capacity**:
The maximum storage assigned to the persistent store, including its own recovery
space. It is distinct from the free space available elsewhere on the host.

**Storage admission range**:
The acceptable minimum and requested maximum for a new persistent store.
Admission fixes one store capacity within that range; later availability does not resize it.

**Host free-space floor**:
The host space that must remain available when Factorybox admits new allocation.
It is an admission condition, not the persistent store's capacity.

**Recovery reserve**:
Storage allocated in advance for completing or rolling back a specific owned
operation when ordinary storage allocation is refused.

**Committed pause**:
A durable complete saved state from which the source environment can be resumed
after the supported loss of its worker.

**Recovery selection**:
A particular saved state deliberately retained as an input for recovery. Its
retention ends only when that selection is explicitly released. Worker
replacement does not release it. Releasing a selection does not withdraw a
committed pause or discard its saved state.

**Checkpoint retirement**:
Permanent withdrawal of a checkpoint's stored backing while its original
request identity remains reserved. It is separate from deleting a sandbox.

**Epoch retirement**:
Permanent withdrawal of an obsolete worker lifetime's stored state after its
live, recovery and descendant dependencies have been resolved.

**Recovery handover**:
An explicit transfer from an old committed pause to an independently preserved
committed pause after verified recovery. Its successor may include independent
work performed after recovery. Releasing a recovery selection alone is not a
handover.

**Retirement maintenance**:
An exclusive period during accepted retirement when operations may not change
the saved inputs needed for safe deletion and recovery. It ends after deletion
and durable completion are confirmed, including after an interrupted attempt.

**Retirement preparation cancellation**:
Withdrawal of an unaccepted retirement attempt's temporary metadata. It leaves
the saved environments intact. It cannot reverse accepted retirement or release
its maintenance protection.
