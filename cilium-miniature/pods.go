package main

import (
	"log/slog"

	"github.com/cilium/hive/cell"
	"github.com/cilium/hive/job"
	"github.com/cilium/statedb"
	"github.com/cilium/statedb/index"
	"k8s.io/client-go/tools/cache"

	"github.com/cilium/cilium/pkg/k8s"
	"github.com/cilium/cilium/pkg/k8s/client"
	slim_corev1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/api/core/v1"
	"github.com/cilium/cilium/pkg/k8s/utils"
)

const PodTableName = "pods"

var (
	podNameIndex = statedb.Index[*slim_corev1.Pod, string]{
		Name: "name",
		FromObject: func(obj *slim_corev1.Pod) index.KeySet {
			return index.NewKeySet(index.String(obj.Namespace + "/" + obj.Name))
		},
		FromKey:    index.String,
		FromString: index.FromString,
		Unique:     true,
	}
	PodByName = podNameIndex.Query
)

func NewPodTable(db *statedb.DB) (statedb.RWTable[*slim_corev1.Pod], error) {
	return statedb.NewTableAny(
		db,
		PodTableName,
		func() []string { return []string{"Namespace", "Name", "PodIP"} },
		func(pod *slim_corev1.Pod) []string {
			return []string{pod.Namespace, pod.Name, pod.Status.PodIP}
		},
		podNameIndex,
	)
}

type PodListerWatcher cache.ListerWatcher

func newPodListerWatcher(log *slog.Logger, cs client.ClientSet) *PodListerWatcher {
	if !cs.IsEnabled() {
		log.Error("k8s client not configured")
		return nil
	}

	return &PodListerWatcher(utils.ListerWatcherFromTyped(cs.Slim().CoreV1().Pods("")))
}

func registerPodReflector(
	jg job.Group,
	lw *PodListerWatcher,
	db *statedb.DB,
	pods statedb.RWTable[*slim_corev1.Pod],
) error {
	if lw == nil {
		return nil
	}

	return k8s.RegisterReflector(jg, db, k8s.ReflectorConfig[*slim_corev1.Pod]{
		Name:          "pods",
		Table:         pods,
		ListerWatcher: lw,
		Transform: func(_ statedb.ReadTxn, obj any) (*slim_corev1.Pod, bool) {
			pod, ok := obj.(*slim_corev1.Pod)
			if !ok {
				return nil, false
			}

			slim := &slim_corev1.Pod{}
			slim.Namespace = pod.Namespace
			slim.Name = pod.Name
			slim.Labels = pod.Labels
			slim.Status.PodIP = pod.Status.PodIP
			slim.Status.Phase = pod.Status.Phase

			return slim, true
		},
	})
}

var PodsCell = cell.Module(
	"pods",
	"Pods reflected from k8s to statedb",
	cell.ProvidePrivate(
		NewPodTable,
		newPodListerWatcher,
	),
	cell.Provide(statedb.RWTable[*slim_corev1.Pod].ToTable),
	cell.Invoke(registerPodReflector),
)
