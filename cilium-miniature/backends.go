package main

import (
	"fmt"

	"github.com/cilium/statedb"
	"github.com/cilium/statedb/index"
	"github.com/cilium/statedb/reconciler"
)

type Backend struct {
	Service string
	PodName string
	PodIP   string
	Port    uint16

	Status reconciler.Status
}

func (b *Backend) Key() string {
	return b.Service + "|" + b.PodIP
}

func (b *Backend) Clone() *Backend {
	b2 := *b
	return &b2
}

func (b *Backend) SetStatus(s reconciler.Status) *Backend {
	b.Status = s
	return b
}

func (b *Backend) GetStatus() reconciler.Status {
	return b.Status
}

const BackendTableName = "backends"

var (
	backendKeyIndex = statedb.Index[*Backend, string]{
		Name: "key",
		FromObject: func(b *Backend) index.KeySet {
			return index.NewKeySet(index.String(b.Key()))
		},
		FromKey:    index.String,
		FromString: index.FromString,
		Unique:     true,
	}
	BackendByKey = backendKeyIndex.Query

	backendServiceIndex = statedb.Index[*Backend, string]{
		Name: "service",
		FromObject: func(b *Backend) index.KeySet {
			return index.NewKeySet(index.String(b.Service))
		},
		FromKey:    index.String,
		FromString: index.FromString,
		Unique:     false,
	}
	BackendByService = backendServiceIndex.Query
)

func NewBackendTable(db *statedb.DB) (statedb.RWTable[*Backend], error) {
	return statedb.NewTableAny(
		db,
		BackendTableName,
		func() []string { return []string{"Service", "Pod", "PodIP", "Port", "Status"} },
		func(b *Backend) []string {
			return []string{
				b.Service,
				b.PodName,
				b.PodIP,
				fmt.Sprintf("%d", b.Port),
				b.Status.String(),
			}
		},
		backendKeyIndex,
		backendServiceIndex,
	)
}
