package pods

import (
	"log/slog"

	"k8s.io/client-go/tools/cache"

	"github.com/cilium/cilium/pkg/k8s/client"
	v1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/api/core/v1"
	"github.com/cilium/cilium/pkg/k8s/utils"
	"github.com/cilium/hive/cell"
	"github.com/cilium/statedb"
	"github.com/cilium/statedb/index"
)

const PodTableName = "pods"

var (
	podNameIndex = statedb.Index[*v1.Pod, string]{
		Name: "name",
		FromObject: func(obj *v1.Pod) index.KeySet {
			return index.NewKeySet(index.String(obj.Namespace + "/" + obj.Name))
		},
		FromKey:    index.String,
		FromString: index.FromString,
		Unique:     true,
	}

	PodByName = podNameIndex.Query
)

func podTableHeader() []string {
	return []string{"Namespace", "Name"}
}

func podTableRow(pod *v1.Pod) []string {
	return []string{pod.Name, pod.Name}
}

func NewPodTable(db *statedb.DB) (statedb.RWTable[*v1.Pod], error) {
	// whats the difference between NewTable() and NewTableAny()?
	return statedb.NewTableAny(
		db,
		PodTableName,
		podTableHeader,
		podTableRow,
		podNameIndex,
	)
}

func newPodsWatcher(log *slog.Logger, cs client.Clientset) cache.ListerWatcher {
	if !cs.IsEnabled() {
		return nil
	}

	return cache.ListerWatcher(utils.ListerWatcherFromTyped(cs.Slim().CoreV1().Pods("")))
}

var PodsCell = cell.Module(
	"pods",
	"pods cell",

	cell.Provide(
		NewPodTable,
		newPodsWatcher,
	),

	cell.Provide(statedb.RWTable[*v1.Pod].ToTable),
	// what happens if reflector not registered?
)
