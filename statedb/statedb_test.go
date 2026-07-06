package bumblebee_test

import (
	"errors"
	"iter"
	"strconv"
	"strings"
	"testing"

	"github.com/cilium/statedb"
	"github.com/cilium/statedb/index"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type Backend struct {
	Name   string
	Port   uint16
	Labels []string
}

func (b *Backend) Clone() *Backend {
	b2 := *b
	return &b2
}

func (b *Backend) TableHeader() []string {
	return []string{"Name", "Port", "Labels"}
}

func (b *Backend) TableRow() []string {
	return []string{b.Name, strconv.FormatUint(uint64(b.Port), 10), strings.Join(b.Labels, ",")}
}

var (
	BackendNameIndex = statedb.Index[*Backend, string]{
		Name: "name",
		FromObject: func(b *Backend) index.KeySet {
			return index.NewKeySet(index.String(b.Name))
		},
		FromKey: index.String,
		Unique:  true,
	}

	BackendPortIndex = statedb.Index[*Backend, uint16]{
		Name: "port",
		FromObject: func(b *Backend) index.KeySet {
			return index.NewKeySet(index.Uint16(b.Port))
		},
		FromKey: index.Uint16,
		Unique:  false,
	}

	BackendLabelIndex = statedb.Index[*Backend, string]{
		Name: "label",
		FromObject: func(b *Backend) index.KeySet {
			return index.StringSlice(b.Labels)
		},
		FromKey: index.String,
		Unique:  false,
	}
)

func newDB(t *testing.T) (*statedb.DB, statedb.RWTable[*Backend]) {
	t.Helper()
	db := statedb.New()
	tbl, error := statedb.NewTable[*Backend](
		db,
		"backends",
		BackendNameIndex,
		BackendPortIndex,
		BackendLabelIndex,
	)

	require.NoError(t, error)
	require.NoError(t, db.Start())

	t.Cleanup(func() { _ = db.Stop() })

	return db, tbl
}

func collect[Obj any](seq iter.Seq2[Obj, statedb.Revision]) []Obj {
	var out []Obj
	for obj := range seq {
		out = append(out, obj)
	}

	return out
}

func TestInsertAndGet(t *testing.T) {
	db, backends := newDB(t)

	txn := db.WriteTxn(backends)
	old, hadOld, err := backends.Insert(txn, &Backend{Name: "be1", Port: 8080})
	require.NoError(t, err)
	assert.False(t, hadOld)
	assert.Nil(t, old)

	old, hadOld, _ = backends.Insert(txn, &Backend{Name: "be1", Port: 9090})
	require.True(t, hadOld)
	assert.Equal(t, old.Port, uint16(8080))
	txn.Commit()

	rtxn := db.ReadTxn()
	got, rev, found := backends.Get(rtxn, BackendNameIndex.Query("be1"))
	assert.Equal(t, found, true)
	assert.Equal(t, got.Port, uint16(9090))
	assert.NotEqual(t, rev, 0)
	assert.Equal(t, backends.NumObjects(rtxn), 1)
}

func TestImmutability(t *testing.T) {
	db, backends := newDB(t)

	obj := &Backend{Name: "immutable", Port: 1}
	txn := db.WriteTxn(backends)

	_, _, err := backends.Insert(txn, obj)
	assert.Nil(t, err)
	txn.Commit()

	txn = db.WriteTxn(backends)
	func() {
		defer func() {
			require.NotNil(t, recover())
		}()

		obj.Port = 2
		_, _, _ = backends.Insert(txn, obj)
	}()
	txn.Abort()

	updated := obj.Clone()
	updated.Port = 2
	txn = db.WriteTxn(backends)

	_, _, err = backends.Insert(txn, updated)
	assert.Nil(t, err)
	txn.Commit()

	got, _, _ := backends.Get(db.ReadTxn(), BackendNameIndex.Query("immutable"))
	assert.Equal(t, got.Port, uint16(2))
}

func TestTxnIsolationAndAbort(t *testing.T) {
	db, backends := newDB(t)

	wtxn := db.WriteTxn(backends)
	_, _, _ = backends.Insert(wtxn, &Backend{Name: "ghost", Port: 1})

	_, _, found := backends.Get(wtxn, BackendNameIndex.Query("ghost"))
	assert.True(t, found)

	_, _, rfound := backends.Get(db.ReadTxn(), BackendNameIndex.Query("ghost"))
	assert.False(t, rfound)

	wtxn.Abort()

	_, _, rfound = backends.Get(db.ReadTxn(), BackendNameIndex.Query("ghost"))
	assert.False(t, rfound)

	_, _, err := backends.Insert(wtxn, &Backend{Name: "late", Port: 2})
	assert.True(t, errors.Is(err, statedb.ErrTransactionClosed))
}
