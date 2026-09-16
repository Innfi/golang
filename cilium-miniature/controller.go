package main

import (
	"context"
	"iter"
	"log/slog"

	"github.com/cilium/hive/cell"
	"github.com/cilium/hive/job"
	"github.com/cilium/statedb"
	"github.com/cilium/statedb/reconciler"

	"github.com/cilium/cilium/pkg/k8s/resource"
	slim_corev1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/api/core/v1"
)

type serviceSpec struct {
	selector map[string]string
	port     uint16
}

type controller struct {
	log       *slog.Logger
	db        *statedb.DB
	pods      statedb.Table[*slim_corev1.Pod]
	services  resource.Resource[*slim_corev1.Service]
	backends  statedb.RWTable[*Backend]
	selectors map[string]serviceSpec
}

func (c *controller) run(ctx context.Context, health cell.Health) error {
	wtxn := c.db.WriteTxn(c.pods)
	podChanges, err := c.pods.Changes(wtxn)
	wtxn.Commit()
	if err != nil {
		return err
	}
	defer podChanges.Close()

	serviceEvents := c.services.Events(ctx)

	initial := make(chan struct{})
	close(initial)
	var podWatch <-chan struct{} = initial

	for {
		select {
		case <-ctx.Done():
			return nil

		case ev, ok := <-serviceEvents:
			if !ok {
				serviceEvents = nil
				continue
			}
			switch ev.Kind {
			case resource.Sync:
			case resource.Upsert:
				key := ev.Key.String()
				if len(ev.Object.Spec.Selector) > 0 && len(ev.Object.Spec.Ports) > 0 {
					c.selectors[key] = serviceSpec{
						selector: ev.Object.Spec.Selector,
						port:     uint16(ev.Object.Spec.Ports[0].Port),
					}
				} else {
					delete(c.selectors, key)
				}
			}

		case <-podWatch:
			var changes iter.Seq2[statedb.Change[*slim_corev1.Pod], statedb.Revision]
			changes, podWatch = podChanges.Next(c.db.ReadTxn())
			for range changes {
				// why no action?
			}
		}

		c.recompute()
		health.OK("reconciled desired backends")
	}
}

func (c *controller) recompute() {
	rtxn := c.db.ReadTxn()

	desired := map[string]*Backend{}
	for svcKey, spec := range c.selectors {
		for pod := range c.pods.All(rtxn) {
			if pod.Status.PodIP == "" {
				continue
			}
			if !selectorMatches(spec.selector, pod.Labels) {
				continue
			}

			b := &Backend{
				Service: svcKey,
				PodName: pod.Namespace + "/" + pod.Name,
				PodIP:   pod.Status.PodIP,
				Port:    spec.port,
			}
			desired[b.Key()] = b
		}
	}

	wtxn := c.db.WriteTxn(c.backends)
	defer wtxn.Commit()

	for key, want := range desired {
		if have, _, ok := c.backends.Get(wtxn, BackendByKey(key)); ok {
			if have.PodName == want.PodName && have.Port == want.Port {
				continue
			}
		}

		want.Status = reconciler.StatusPending()
		c.backends.Insert(wtxn, want)
		c.log.Info("desired backend", "service", want.Service, "ip", want.PodIP)
	}

	// reconciler follows what table dictates
	for have := range c.backends.All(wtxn) {
		if _, ok := desired[have.Key()]; !ok {
			c.backends.Delete(wtxn, have)
			c.log.Info("removing backend", "service", have.Service, "ip", have.PodIP)
		}
	}
}

func selectorMatches(selector, labels map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func registerController(
	jg job.Group,
	log *slog.Logger,
	db *statedb.DB,
	pods statedb.Table[*slim_corev1.Pod],
	services resource.Resource[*slim_corev1.Service],
	backends statedb.RWTable[*Backend],
) {
	if services == nil {
		return
	}

	c := &controller{
		log:       log,
		db:        db,
		pods:      pods,
		services:  services,
		backends:  backends,
		selectors: map[string]serviceSpec{},
	}

	jg.Add(job.OneShot("backend-controller", c.run))
}
