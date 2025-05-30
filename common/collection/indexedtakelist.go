package collection

// IndexedTakeList holds a set of values that can only be observed by being
// removed from the set. It is possible for this set to contain duplicate values
// as long as each value maps to a distinct index.
type (
	IndexedTakeList[K comparable, V any] struct {
		values []kv[K, V]
	}

	kv[K comparable, V any] struct {
		key     K
		value   V
		removed bool
	}
)

// NewIndexedTakeList constructs a new IndexedTakeSet by applying the provided
// indexer to each of the provided values.
func NewIndexedTakeList[K comparable, V any](
	values []V,
	indexer func(V) K,
) *IndexedTakeList[K, V] {
	ret := &IndexedTakeList[K, V]{
		values: make([]kv[K, V], 0, len(values)),
	}
	for _, v := range values {
		ret.values = append(ret.values, kv[K, V]{key: indexer(v), value: v})
	}
	return ret
}

// Take finds a value in this set by its key and removes it, returning the
// value.
func (itl *IndexedTakeList[K, V]) Take(key K) (V, bool) {
	var zero V
	for i := 0; i < len(itl.values); i++ {
		kv := &itl.values[i]
		if kv.key != key {
			continue
		}
		if kv.removed {
			return zero, false
		}
		kv.removed = true
		return kv.value, true
	}
	return zero, false
}

// TakeRemaining removes all remaining values from this set and returns them.
func (itl *IndexedTakeList[K, V]) TakeRemaining() []V {
	out := make([]V, 0, len(itl.values))
	for i := 0; i < len(itl.values); i++ {
		kv := &itl.values[i]
		if !kv.removed {
			out = append(out, kv.value)
		}
	}
	itl.values = nil
	return out
}
