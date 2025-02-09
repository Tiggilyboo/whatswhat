package view

import "sync"

type ViewStack struct {
	lock    sync.Mutex
	history []Message
}

func NewViewStack() *ViewStack {
	return &ViewStack{
		lock:    sync.Mutex{},
		history: []Message{},
	}
}

func (s *ViewStack) Push(v Message) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.history = append(s.history, v)
}

func (s *ViewStack) Len() int {
	return len(s.history)
}

func (s *ViewStack) Pop() Message {
	s.lock.Lock()
	defer s.lock.Unlock()

	l := len(s.history)
	if l == 0 {
		return Undefined
	}

	v := s.history[l-1]
	s.history = s.history[:l-1]
	return v
}

func (s *ViewStack) Peek() Message {
	l := len(s.history)
	if l == 0 {
		return Undefined
	}

	return s.history[l-1]
}

func (s *ViewStack) Clear() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.history = []Message{}
}
