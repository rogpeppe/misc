package auth

import (
	"crypto/sha256"
	"fmt"
	"sort"
)

// Prefixes for entity names. All entity types should be
// distinguished with a unique prefix (all the characters
// up until the first hyphen, or the end of the string, whichever
// comes first).
//
// Entity names must not contain space characters.
const (
	// LoginEntity represents the part of the service that requires
	// authentication. If an operation specifies this entity, the
	// client must authenticate themselves even if they otherwise
	// have capabilities that allow them to perform the operation.
	// There is only one such entity.
	LoginEntity = "login"

	// MultiOpEntityPrefix is the prefix used for entities that represent
	// a set of operations. The actual operations
	// associated with the entity will be stored in the MultiKeyStore.
	MultiOpEntityPrefix = "multi"
)

// NewMultiEntity returns a new multi-op entity name that represents
// all the given operations and caveats. It returns the same value regardless
// of the ordering of the operations. It also sorts the caveats and returns
// the operations sorted with duplicates removed.
//
// An unattenuated macaroon that has an id with a given multi-op key
// can be used to authorize any or all of the operations, assuming
// the value can be found in the MultiOpStore.
func NewMultiOpEntity(ops []Op) (string, []Op) {
	sort.Sort(opsByValue(ops))
	// Hash the operations, removing duplicates as we go.
	h := sha256.New()
	var prevOp Op
	var data []byte
	j := 0
	for i, op := range ops {
		if i > 0 && op == prevOp {
			// It's a duplicate - ignore.
			continue
		}
		data = data[:0]
		data = append(data, op.Action...)
		data = append(data, '\n')
		data = append(data, op.Entity...)
		data = append(data, '\n')
		h.Write(data)
		ops[j] = op
		j++
		prevOp = op
	}

	return fmt.Sprintf("%s-%x", MultiOpEntityPrefix, h.Sum(data[:0])), ops[0:j]
}

type opsByValue []Op

func (o opsByValue) Less(i, j int) bool {
	o0, o1 := o[i], o[j]
	if o0.Entity != o1.Entity {
		return o0.Entity < o1.Entity
	}
	return o0.Action < o1.Action
}

func (o opsByValue) Swap(i, j int) {
	o[i], o[j] = o[j], o[i]
}

func (o opsByValue) Len() int {
	return len(o)
}
