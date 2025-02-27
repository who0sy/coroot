package watchers

import (
	"context"
	"github.com/coroot/coroot/cache"
	"github.com/coroot/coroot/cloud-pricing"
	"github.com/coroot/coroot/constructor"
	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/timeseries"
	"k8s.io/klog"
	"sync"
)

func Start(db *db.DB, cache *cache.Cache, pricing *cloud_pricing.Manager, checkIncidents, checkDeployments bool) {
	var incidents *Incidents
	if checkIncidents {
		incidents = NewIncidents(db)
	}
	var deployments *Deployments
	if checkDeployments {
		deployments = NewDeployments(db, pricing)
	}

	if incidents == nil && deployments == nil {
		return
	}

	go func() {
		for projectId := range cache.Updates() {
			project, err := db.GetProject(projectId)
			if err != nil {
				klog.Errorln(err)
				continue
			}
			cacheClient := cache.GetCacheClient(project.Id)
			cacheTo, err := cacheClient.GetTo()
			if err != nil {
				klog.Errorln(err)
				continue
			}
			if cacheTo.IsZero() {
				continue
			}
			to := cacheTo
			from := to.Add(-timeseries.Hour)
			step, err := cacheClient.GetStep(from, to)
			if err != nil {
				klog.Errorln(err)
				continue
			}
			ctr := constructor.New(db, project, cacheClient, pricing)
			world, err := ctr.LoadWorld(context.TODO(), from, to, step, nil)
			if err != nil {
				klog.Errorln("failed to load world:", err)
				continue
			}

			wg := sync.WaitGroup{}
			if incidents != nil {
				wg.Add(1)
				go func() {
					defer wg.Done()
					incidents.Check(project, world)
				}()
			}
			if deployments != nil {
				wg.Add(1)
				go func() {
					defer wg.Done()
					deployments.Check(project, world)
				}()
			}
			wg.Wait()
		}
	}()
}
