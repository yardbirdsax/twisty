package osmconv

import (
	"container/heap"
	"sync"
)

// MergeScanners returns an ObjectScanner that performs an N-way sorted merge
// of pre-sorted ObjectScanners. Deduplicates consecutive objects with the
// same (Type, ID). Accepts the interface so any scanner implementation works.
func MergeScanners(scanners ...ObjectScanner) ObjectScanner {
	return &mergeScanner{scanners: scanners}
}

type mergeScanner struct {
	scanners []ObjectScanner
	h        mergeHeap
	once     sync.Once
	current  Object
	err      error
	lastType ObjectType
	lastID   int64
	hasLast  bool
}

func (m *mergeScanner) init() {
	for i, s := range m.scanners {
		if s.Next() {
			heap.Push(&m.h, &heapEntry{obj: s.Object(), idx: i})
		} else if err := s.Err(); err != nil {
			m.err = err
			return
		}
	}
}

func (m *mergeScanner) Next() bool {
	m.once.Do(func() { m.init() })
	if m.err != nil {
		return false
	}

	for m.h.Len() > 0 {
		entry := heap.Pop(&m.h).(*heapEntry)
		obj := entry.obj

		// Advance the scanner this object came from
		s := m.scanners[entry.idx]
		if s.Next() {
			entry.obj = s.Object()
			heap.Push(&m.h, entry)
		} else if err := s.Err(); err != nil {
			m.err = err
			return false
		}

		// Deduplicate
		if m.hasLast && obj.Type == m.lastType && obj.ID == m.lastID {
			continue
		}

		m.current = obj
		m.lastType = obj.Type
		m.lastID = obj.ID
		m.hasLast = true
		return true
	}
	return false
}

func (m *mergeScanner) Object() Object {
	return m.current
}

func (m *mergeScanner) Err() error {
	return m.err
}

// heapEntry holds a pending object and which scanner it came from.
type heapEntry struct {
	obj Object
	idx int
}

type mergeHeap []*heapEntry

func (h mergeHeap) Len() int { return len(h) }

func (h mergeHeap) Less(i, j int) bool {
	return ObjectLess(h[i].obj, h[j].obj)
}

func (h mergeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *mergeHeap) Push(x any) {
	*h = append(*h, x.(*heapEntry))
}

func (h *mergeHeap) Pop() any {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return entry
}
