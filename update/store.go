package update

// store 是一个基于稀疏集合的组件存储。
//
// entities 和 values 保持密集排列，子系统遍历时无需 map 查找；
// index 提供 O(1) 随机访问。
//
// 内部不做并发保护，因为 World 只有单一写入者（Step goroutine）。
type store[T any] struct {
	entities []EntityID       // 密集存储的实体 ID 列表
	values   []T              // 与 entities 一一对应的组件值
	index    map[EntityID]int // 实体 ID → entities/values 下标
}

// newStore 创建指定初始容量的组件存储。
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

// len 返回当前存储的实体数量。
func (s *store[T]) len() int {
	return len(s.entities)
}

// has 判断实体是否有该组件数据。
func (s *store[T]) has(id EntityID) bool {
	_, ok := s.index[id]
	return ok
}

// get 返回实体组件数据的指针，未找到时返回 nil。
// 返回的指针仅在下一次对该实体的 set/remove 调用前有效。
func (s *store[T]) get(id EntityID) (*T, bool) {
	i, ok := s.index[id]
	if !ok {
		return nil, false
	}
	return &s.values[i], true
}

// set 写入实体的组件数据。已存在则原地更新，否则追加到密集列表尾部。
func (s *store[T]) set(id EntityID, value T) {
	if i, ok := s.index[id]; ok {
		s.values[i] = value
		return
	}
	s.index[id] = len(s.entities)
	s.entities = append(s.entities, id)
	s.values = append(s.values, value)
}

// remove 删除实体的组件数据。使用 swap-and-pop 技巧保持密集存储，
// 将最后一个元素移到被删除位置，然后截断切片，避免空洞。
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
