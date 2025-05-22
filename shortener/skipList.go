package shortener

import (
	"errors"
	"fmt"
	"iter"
	"math/rand/v2"
	"sync/atomic"
)

const MAX_LEVEL = 31

type markablePointer struct {
	ref  *node
	mark bool
}

type atomicMarkablePointer struct {
	value atomic.Value
}

func (amp *atomicMarkablePointer) Store(pointer markablePointer) {
	amp.value.Store(pointer)
}

func (amp *atomicMarkablePointer) Load() markablePointer {
	if amp.value.Load() == nil {
		return markablePointer{mark: false, ref: nil}
	}
	return amp.value.Load().(markablePointer)
}

func (amp *atomicMarkablePointer) CompareAndSwap(oldRef *node, oldMark bool, newRef *node, newMark bool) bool {
	return amp.value.CompareAndSwap(
		markablePointer{ref: oldRef, mark: oldMark},
		markablePointer{ref: newRef, mark: newMark},
	)
}

func (amp *atomicMarkablePointer) IsMarked() bool {
	return amp.Load().mark
}

func (amp *atomicMarkablePointer) SetMark(mark bool) {
	amp.value.Swap(markablePointer{mark: mark, ref: amp.Load().ref})
}

func (amp *atomicMarkablePointer) Ref() *node {
	return amp.Load().ref
}

func (amp *atomicMarkablePointer) SetRef(ref *node) {
	amp.value.Swap(markablePointer{mark: amp.Load().mark, ref: ref})
}

type node struct {
	key    string
	value  string
	height int
	next   [MAX_LEVEL + 1]atomicMarkablePointer
}

type nodeTrace = [MAX_LEVEL + 1]*node

type SkipList struct {
	head *node
	tail *node
}

func NewSkiplist() *SkipList {
	head := newNode("", "[HEAD]", MAX_LEVEL)
	tail := newNode("\U0010ffff[TAIL]", "[TAIL]", MAX_LEVEL)
	for i := range MAX_LEVEL + 1 {
		head.next[i].SetRef(tail)
	}
	return &SkipList{head: head, tail: tail}
}

func newNode(key string, value string, height int) *node {
	res := &node{
		key:    key,
		value:  value,
		height: height,
	}
	for i := range height + 1 {
		res.next[i].Store(markablePointer{ref: nil, mark: false})
	}
	return res
}

func (s *SkipList) find(key string) (pred *nodeTrace, succ *nodeTrace, found bool) {
	pred = new(nodeTrace)
	succ = new(nodeTrace)

RETRY:
	for {
		last := s.head
		current := s.head
		for level := MAX_LEVEL; level >= 0; level-- {
			current = last.next[level].Ref()
			for {
				next := current.next[level].Load()

				for next.mark {
					snipped := last.next[level].CompareAndSwap(current, false, next.ref, false)
					if !snipped {
						continue RETRY
					}
					current = last.next[level].Ref()
					next = current.next[level].Load()
				}

				if current.key >= key {
					break
				}

				last = current
				current = next.ref
			}
			pred[level] = last
			succ[level] = current
		}
		return pred, succ, (current.key == key)
	}
}

func generateHeight() int {
	height := 0
	for rand.IntN(2) == 1 && height <= MAX_LEVEL {
		height++
	}
	return height
}

func (s *SkipList) Insert(key string, value string) error {

	height := generateHeight()
	newNode := newNode(key, value, height)

	for {
		pred, succ, found := s.find(key)
		if found {
			return errors.New("Key already exists")
		}

		// setup new node
		for level := range height + 1 {
			newNode.next[level].SetRef(succ[level])
		}

		// insert into SkipList
		last := pred[0]
		next := succ[0]
		if !last.next[0].CompareAndSwap(next, false, newNode, false) {
			// retry whole opperation
			continue
		}

		// insert in upper levels
		for i := 1; i <= height; i++ {
			// CAS loop
			for {
				last = pred[i]
				next = succ[i]
				if last.next[i].CompareAndSwap(next, false, newNode, false) {
					break
				}
				pred, succ, _ = s.find(key)
			}
		}
		return nil
	}

}

func (s *SkipList) Search(key string) (bool, string) {
	last := s.head
	current := s.head
	for level := MAX_LEVEL; level >= 0; level-- {
		current = last.next[level].Ref()
		for {
			next := current.next[level].Load()

			for next.mark {
				current = current.next[level].Ref()
				next = current.next[level].Load()
			}

			if current.key >= key {
				break
			}

			last = current
			current = next.ref
		}
	}

	return (current.key == key), current.value
}

func (s *SkipList) Print() {
	for i := s.head.next[0].Ref(); i != s.tail; i = i.next[0].Ref() {
		fmt.Println(i.key, i.value)
	}
}

func (s *SkipList) Iter() iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		for i := s.head.next[0]; i.Ref() != s.tail; i = i.Ref().next[0] {
			if i.IsMarked() {
				continue
			}
			if !yield(i.Ref().key, i.Ref().value) {
				return
			}
		}
	}
}

func (s *SkipList) Remove(id string) error {
	_, succ, found := s.find(id)
	if !found {
		return errors.New(fmt.Sprintf("ID '%s' NOT delete! ID could not be found!", id))
	}
	nodeToRemove := succ[0]
	for i := nodeToRemove.height; i >= 1; i-- {
		next := nodeToRemove.next[i]
		for !next.IsMarked() {
			nodeToRemove.next[i].CompareAndSwap(next.Ref(), false, next.Ref(), true)
			next = nodeToRemove.next[i]
		}
	}
	next := nodeToRemove.next[0]
	for {
		iMarkedIt := nodeToRemove.next[0].CompareAndSwap(next.Ref(), false, next.Ref(), true)
		next := nodeToRemove.next[0]
		if iMarkedIt {
			s.find(id)
			return nil
		} else {
			if next.IsMarked() {
				return errors.New(fmt.Sprintf("ID '%s', already deleted!", id))
			}
		}
	}
}
