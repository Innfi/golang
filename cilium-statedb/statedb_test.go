package bumblebee_test

import (
	"context"
	"errors"
	"iter"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cilium/hive"
	"github.com/cilium/hive/cell"
	"github.com/cilium/hive/hivetest"
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

func TestSecondaryIndexes(t *testing.T) {
	db, backends := newDB(t)

	wtxn := db.WriteTxn(backends)
	for _, b := range []*Backend{
		{Name: "web-1", Port: 80, Labels: []string{"web", "prod"}},
		{Name: "web-2", Port: 80, Labels: []string{"web", "canary"}},
		{Name: "api-1", Port: 8080, Labels: []string{"api", "prod"}},
	} {
		_, _, err := backends.Insert(wtxn, b)
		require.NoError(t, err)
	}
	wtxn.Commit()
	rtxn := db.ReadTxn()

	port80 := collect(backends.List(rtxn, BackendPortIndex.Query(80)))
	assert.Equal(t, len(port80), 2)

	prod := collect(backends.List(rtxn, BackendLabelIndex.Query("canary")))
	assert.Equal(t, len(prod), 1)

	got := collect(backends.Prefix(rtxn, BackendNameIndex.Query("web-")))
	assert.Equal(t, len(got), 2)
}

func TestRevisions(t *testing.T) {
	db, backends := newDB(t)

	wtxn := db.WriteTxn(backends)
	// is underscore mandatory?
	_, _, _ = backends.Insert(wtxn, &Backend{Name: "a", Port: 1})
	_, _, _ = backends.Insert(wtxn, &Backend{Name: "b", Port: 2})

	rtxn := wtxn.Commit()

	_, revA, _ := backends.Get(rtxn, BackendNameIndex.Query("a"))
	_, revB, _ := backends.Get(rtxn, BackendNameIndex.Query("b"))
	require.True(t, revB > revA)

	tableRev := backends.Revision(rtxn)
	require.Equal(t, tableRev, revB)

	wtxn = db.WriteTxn(backends)
	_, _, _ = backends.Insert(wtxn, &Backend{Name: "a", Port: 100})
	rtxn = wtxn.Commit()

	test := statedb.ByRevision[*Backend](revB + 1)
	changed := collect(backends.LowerBound(rtxn, test))

	assert.Equal(t, len(changed), 1)
	assert.Equal(t, changed[0].Name, "a")
	assert.Equal(t, changed[0].Port, uint16(100))
}

func TestWatchChannels(t *testing.T) {
	db, backends := newDB(t)

	wtxn := db.WriteTxn(backends)
	_, _, _ = backends.Insert(wtxn, &Backend{Name: "watched", Port: 1})
	wtxn.Commit()

	_, _, watch, found := backends.GetWatch(db.ReadTxn(), BackendNameIndex.Query("watched"))
	require.True(t, found)

	select {
	case <-watch:
		t.Fatal("watch closed before any write")
	default:
	}

	wtxn = db.WriteTxn(backends)
	_, _, _ = backends.Insert(wtxn, &Backend{Name: "watched", Port: 2})
	wtxn.Commit()

	select {
	case <-watch:
	case <-time.After(time.Second):
		t.Fatal("watch channel did no t close after the object changed")
	}
}

func TestChangesIterator(t *testing.T) {
	db, backends := newDB(t)

	wtxn := db.WriteTxn(backends)
	changeIter, err := backends.Changes(wtxn)
	require.NoError(t, err)

	defer changeIter.Close()
	wtxn.Commit()

	wtxn = db.WriteTxn(backends)
	_, _, _ = backends.Insert(wtxn, &Backend{Name: "x", Port: 1})
	_, _, _ = backends.Insert(wtxn, &Backend{Name: "y", Port: 2})
	wtxn.Commit()

	wtxn = db.WriteTxn(backends)
	_, _, _ = backends.Insert(wtxn, &Backend{Name: "x", Port: 10})
	_, _, _ = backends.Delete(wtxn, &Backend{Name: "y"})
	wtxn.Commit()

	upserts := map[string]uint16{}
	deletes := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for {
		changes, watch := changeIter.Next(db.ReadTxn())
		for change := range changes {
			if change.Deleted {
				deletes[change.Object.Name] = true
			} else {
				upserts[change.Object.Name] = change.Object.Port
			}
		}
		if len(upserts) >= 1 && len(deletes) >= 1 {
			break
		}

		select {
		case <-deadline:
			t.Fatalf("timeout; upserts=%v deletes=%v", upserts, deletes)
		case <-watch:
		}
	}

	assert.Equal(t, upserts["x"], uint16(10))
	assert.True(t, deletes["y"])
}

func TestCompareAndSwap(t *testing.T) {
	db, backends := newDB(t)

	wtxn := db.WriteTxn(backends)
	_, _, _ = backends.Insert(wtxn, &Backend{Name: "cas", Port: 1})
	rtxn := wtxn.Commit()
	_, rev, _ := backends.Get(rtxn, BackendNameIndex.Query("cas"))

	wtxn = db.WriteTxn(backends)
	_, _, err := backends.CompareAndSwap(wtxn, rev, &Backend{Name: "cas", Port: 2})
	require.NoError(t, err)
	wtxn.Commit()

	wtxn = db.WriteTxn(backends)
	defer wtxn.Abort()
	_, _, casErr := backends.CompareAndSwap(wtxn, rev, &Backend{Name: "cas", Port: 3})
	assert.True(t, errors.Is(casErr, statedb.ErrRevisionNotEqual))
}

func TestInitializers(t *testing.T) {
	db, backends := newDB(t)

	wtxn := db.WriteTxn(backends)
	doneK8s := backends.RegisterInitializer(wtxn, "k8s-sync")
	doneRest := backends.RegisterInitializer(wtxn, "restore")
	wtxn.Commit()

	init, _ := backends.Initialized(db.ReadTxn())
	require.False(t, init)

	wtxn = db.WriteTxn(backends)
	doneK8s(wtxn)
	wtxn.Commit()
	init, _ = backends.Initialized(db.ReadTxn())
	require.False(t, init)

	wtxn = db.WriteTxn(backends)
	doneRest(wtxn)
	wtxn.Commit()

	init, _ = backends.Initialized(db.ReadTxn())
	assert.True(t, init)
}

func TestHiveIntegration(t *testing.T) {
	newTableCell := func(db *statedb.DB) (statedb.RWTable[*Backend], error) {
		return statedb.NewTable(db, "backends", BackendNameIndex, BackendPortIndex, BackendLabelIndex)
	}

	var (
		gotDB    *statedb.DB
		gotTable statedb.Table[*Backend]
	)

	h := hive.New(
		statedb.Cell,

		cell.Module(
			"backends",
			"Backend table",
			cell.ProvidePrivate(newTableCell),
			cell.Provide(statedb.RWTable[*Backend].ToTable),
		),

		cell.Invoke(func(db *statedb.DB, tbl statedb.Table[*Backend]) {
			gotDB, gotTable = db, tbl
		}),
	)

	log := hivetest.Logger(t)
	err := h.Start(log, context.Background())
	require.NoError(t, err)
	defer func() {
		err = h.Stop(log, context.Background())
		require.NoError(t, err)
	}()

	n := gotTable.NumObjects(gotDB.ReadTxn())
	assert.True(t, n == 0)
	assert.Equal(t, gotTable.Name(), "backends")
}
