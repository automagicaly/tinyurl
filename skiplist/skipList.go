package skiplist

import (
	"errors"
	"fmt"
	"iter"
	"math/rand/v2"
	"sync/atomic"
)

const MAX_LEVEL = 31

type markablePointer[T any] struct {
	ref  *node[T]
	mark bool
}

type atomicMarkablePointer[T any] struct {
	value atomic.Value
}

func (amp *atomicMarkablePointer[T]) Store(pointer markablePointer[T]) {
	amp.value.Store(pointer)
}

func (amp *atomicMarkablePointer[T]) Load() markablePointer[T] {
	if amp.value.Load() == nil {
		return markablePointer[T]{mark: false, ref: nil}
	}
	return amp.value.Load().(markablePointer[T])
}

func (amp *atomicMarkablePointer[T]) CompareAndSwap(oldRef *node[T], oldMark bool, newRef *node[T], newMark bool) bool {
	return amp.value.CompareAndSwap(
		markablePointer[T]{ref: oldRef, mark: oldMark},
		markablePointer[T]{ref: newRef, mark: newMark},
	)
}

func (amp *atomicMarkablePointer[T]) IsMarked() bool {
	return amp.Load().mark
}

func (amp *atomicMarkablePointer[T]) SetMark(mark bool) {
	amp.value.Swap(markablePointer[T]{mark: mark, ref: amp.Load().ref})
}

func (amp *atomicMarkablePointer[T]) Ref() *node[T] {
	return amp.Load().ref
}

func (amp *atomicMarkablePointer[T]) SetRef(ref *node[T]) {
	amp.value.Swap(markablePointer[T]{mark: amp.Load().mark, ref: ref})
}

type node[T any] struct {
	key    string
	value  T
	height int
	next   [MAX_LEVEL + 1]atomicMarkablePointer[T]
}

type nodeTrace[T any] = [MAX_LEVEL + 1]*node[T]

type SkipList[T any] struct {
	head *node[T]
	tail *node[T]
}

func NewSkiplist[T any]() *SkipList[T] {
	head := newNode[T]("", *new(T), MAX_LEVEL)
	tail := newNode[T]("\U0010ffff[TAIL]", *new(T), MAX_LEVEL)
	for i := range MAX_LEVEL + 1 {
		head.next[i].SetRef(tail)
	}
	return &SkipList[T]{head: head, tail: tail}
}

func newNode[T any](key string, value T, height int) *node[T] {
	res := &node[T]{
		key:    key,
		value:  value,
		height: height,
	}
	for i := range height + 1 {
		res.next[i].Store(markablePointer[T]{ref: nil, mark: false})
	}
	return res
}

func (s *SkipList[T]) find(key string) (pred *nodeTrace[T], succ *nodeTrace[T], found bool) {
	pred = new(nodeTrace[T])
	succ = new(nodeTrace[T])

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

func (s *SkipList[T]) Insert(key string, value T) error {

	height := generateHeight()
	newNode := newNode[T](key, value, height)

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

func (s *SkipList[T]) Search(key string) (bool, T) {
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

func (s *SkipList[T]) Print() {
	for i := s.head.next[0].Ref(); i != s.tail; i = i.next[0].Ref() {
		fmt.Println(i.key, i.value)
	}
}

func (s *SkipList[T]) Iter() iter.Seq2[string, T] {
	return func(yield func(string, T) bool) {
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

func (s *SkipList[T]) Remove(id string) error {
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
