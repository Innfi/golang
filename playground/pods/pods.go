package pods

import (
	"log/slog"

	"k8s.io/client-go/tools/cache"

	"github.com/cilium/cilium/pkg/k8s/client"
	"github.com/cilium/cilium/pkg/k8s/utils"
	"github.com/cilium/hive/cell"
)

func newPodsWatcher(log *slog.Logger, cs client.Clientset) cache.ListerWatcher {
	if !cs.IsEnabled() {
		return nil
	}

	return cache.ListerWatcher(utils.ListerWatcherFromTyped(cs.Slim().CoreV1().Pods("")))
}

var PodsCell = cell.Module(
	"pods",
	"pods cell",

	cell.Provide(newPodsWatcher),
)
