package bumblebee_test

import (
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

func newBackendTable(t *testing.T, db *statedb.DB) statedb.RWTable[*Backend] {
	t.Helper()
	tbl, error := statedb.NewTable[*Backend](
		db,
		"backends",
		BackendNameIndex,
		BackendPortIndex,
		BackendLabelIndex,
	)

	require.NoError(t, error)

	return tbl
}

func newDB(t *testing.T) (*statedb.DB, statedb.RWTable[*Backend]) {
	t.Helper()
	db := statedb.New()
	tbl := newBackendTable(t, db)

	//FIXME: old example
	require.NoError(t, db.RegisterTable(tbl))
	require.NoError(t, db.Start())

	t.Cleanup(func() { _ = db.Stop() })

	return db, tbl
}

func collect[Obj any](it statedb.Iterator[Obj]) []Obj {
	var out []Obj
	for obj, _, ok := it.Next(); ok; obj, _, ok = it.Next() {
		out = append(out, obj)
	}

	return out
}

func TestInsertAndGet(t *testing.T) {
	db, backends := newDB(t)

	txn := db.WriteTxn(backends)
	old, hadOld, err := backends.Insert(txn, &Backend{Name: "be1", Port: 8080})
	require.NoError(t, err)
	require.False(t, hadOld)
	require.NoError(t, old)

	old, hadOld, _ = backends.Insert(txn, &Backend{Name: "be1", Port: 9090})
	require.False(t, hadOld)
	assert.Equal(t, old.Port, 8080)
	txn.Commit()

	rtxn := db.ReadTxn()
	got, rev, found := backends.Get(rtxn, BackendNameIndex.Query("be1"))
	assert.Equal(t, found, true)
	assert.Equal(t, got.Port, 9090)
	assert.NotEqual(t, rev, 0)
	assert.Equal(t, backends.NumObjects(rtxn), 1)
}
