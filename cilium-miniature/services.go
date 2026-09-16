package main

import (
	"github.com/cilium/hive/cell"
	"k8s.io/client-go/util/workqueue"

	"github.com/cilium/cilium/pkg/k8s/client"
	"github.com/cilium/cilium/pkg/k8s/resource"
	slim_corev1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/api/core/v1"
	"github.com/cilium/cilium/pkg/k8s/utils"
)

// service fore resource.Resource[T]
func newServiceResource(
	lc cell.Lifecycle,
	cs client.Clientset,
	mp workqueue.MetricsProvider,
) resource.Resource[*slim_corev1.Service] {
	if !cs.IsEnabled() {
		return nil
	}

	lw := utils.ListerWatcherFromTyped[*slim_corev1.ServiceList](
		cs.Slim().CoreV1().Services(""),
	)

	return resource.New[*slim_corev1.Service](
		lc, lw, mp,
		resource.WithMetric("Service"),
	)
}

var ServicesCell = cell.Module(
	"services",
	"services observed as a Resource[T] event stream",
	cell.Provide(newServiceResource),
)
