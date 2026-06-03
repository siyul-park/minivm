package asm

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

// Data is a writable mmap region used for runtime-patched indirection
// slots. Unlike Buffer it never flips to executable, so atomic stores into
// it stay live to running native code without TLB or icache concerns.
//
// All slots are pointer-sized. Concurrent Set calls are safe; allocation
// serializes on an internal lock.
type Data struct {
	mu     sync.Mutex
	old    []memory // retired regions kept alive; compiled code holds raw pointers into them
	mem    memory
	offset int
}

// NewData allocates a fresh writable data region of the given capacity,
// rounded up to a page.
func NewData(size int) (*Data, error) {
	mem, err := allocMemory(size)
	if err != nil {
		return nil, err
	}
	return &Data{mem: mem}, nil
}

// Alloc reserves one pointer-sized slot and returns its address. The slot's
// initial contents are zero. The returned pointer is stable for the lifetime
// of the Data region.
func (d *Data) Alloc() (unsafe.Pointer, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	const slotSize = int(unsafe.Sizeof(uintptr(0)))
	end := d.offset + slotSize
	if end > len(d.mem) {
		if err := d.grow(end); err != nil {
			return nil, err
		}
		end = d.offset + slotSize
	}
	ptr := unsafe.Pointer(&d.mem[d.offset])
	d.offset = end
	return ptr, nil
}

// Set atomically stores ptr into slot. Slot must have come from Alloc on
// this Data.
func (d *Data) Set(slot unsafe.Pointer, ptr unsafe.Pointer) {
	atomic.StorePointer((*unsafe.Pointer)(slot), ptr)
}

// Load atomically reads the pointer currently stored at slot.
func (d *Data) Load(slot unsafe.Pointer) unsafe.Pointer {
	return atomic.LoadPointer((*unsafe.Pointer)(slot))
}

// Free releases all underlying mmap regions.
func (d *Data) Free() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, r := range d.old {
		if err := r.free(); err != nil {
			return err
		}
	}
	d.old = nil
	return d.mem.free()
}

func (d *Data) grow(need int) error {
	size := len(d.mem) * 2
	if size < need {
		size = need
	}
	mem, err := allocMemory(size)
	if err != nil {
		return err
	}
	// Retire current region without freeing: Alloc pointers baked into
	// compiled native code still reference addresses inside it.
	d.old = append(d.old, d.mem)
	d.mem = mem
	d.offset = 0
	return nil
}
