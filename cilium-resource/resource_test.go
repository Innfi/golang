package bumblebee_test

import (
	"context"
	"testing"
	"time"

	"github.com/cilium/hive/cell"

	k8sClient "github.com/cilium/cilium/pkg/k8s/client"
	"github.com/cilium/cilium/pkg/k8s/resource"
	"github.com/cilium/cilium/pkg/k8s/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit(t *testing.T) {
	require.Equal(t, 1, 1)

	assert.Equal(t, 1, 1)
}

const labTimeOut = 30 * time.Second

func labCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), labTimeOut)
	t.Cleanup(cancel)

	return ctx
}

func node(name, phase string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status:     corev1.NodeStatus{Phase: corev1.NodePhase(phase)},
	}
}

var nodesResource = cell.Provide(
	func(lc cell.Lifecycle, c k8sClient.Clientset) resource.Resource[*corev1.Node] {
		lw := utils.ListerWatcherFromTyped[*corev1.NodeList](c.CoreV1().Nodes())

		return resource.New[*corev1.Node](lc, lw, nil)
	},
)
