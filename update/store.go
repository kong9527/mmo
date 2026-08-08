package update

// store is a sparse-set component store. Values and entity IDs stay dense so
// systems can iterate without map lookups; index provides O(1) direct access.
// It is intentionally not thread-safe because a World has a single writer.
type store[T any] struct {
	entities []EntityID
	values   []T
	index    map[EntityID]int
}

func newStore[T any](capacity int) *store[T] {
	if capacity < 0 {
		capacity = 0
	}
	return &store[T]{
		entities: make([]EntityID, 0, capacity),
		values:   make([]T, 0, capacity),
		index:    make(map[EntityID]int, capacity),
	}
}

func (s *store[T]) len() int {
	return len(s.entities)
}

func (s *store[T]) has(id EntityID) bool {
	_, ok := s.index[id]
	return ok
}

func (s *store[T]) get(id EntityID) (*T, bool) {
	i, ok := s.index[id]
	if !ok {
		return nil, false
	}
	return &s.values[i], true
}

func (s *store[T]) set(id EntityID, value T) {
	if i, ok := s.index[id]; ok {
		s.values[i] = value
		return
	}
	s.index[id] = len(s.entities)
	s.entities = append(s.entities, id)
	s.values = append(s.values, value)
}

func (s *store[T]) remove(id EntityID) bool {
	i, ok := s.index[id]
	if !ok {
		return false
	}

	last := len(s.entities) - 1
	if i != last {
		s.entities[i] = s.entities[last]
		s.values[i] = s.values[last]
		s.index[s.entities[i]] = i
	}

	delete(s.index, id)
	var zero T
	s.entities[last] = 0
	s.values[last] = zero
	s.entities = s.entities[:last]
	s.values = s.values[:last]
	return true
}
