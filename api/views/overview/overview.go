package overview

import (
	"github.com/coroot/coroot/model"
)

type Overview struct {
	Health      []*ApplicationStatus `json:"health"`
	Map         []*Application       `json:"map"`
	Nodes       *model.Table         `json:"nodes"`
	Deployments []*Deployment        `json:"deployments"`
	Costs       *Costs               `json:"costs"`
}

func Render(w *model.World, view string) *Overview {
	v := &Overview{}

	for _, n := range w.Nodes {
		if n.Price != nil {
			v.Costs = &Costs{}
			break
		}
	}

	switch view {
	case "health":
		v.Health = renderHealth(w)
	case "map":
		v.Map = renderServiceMap(w)
	case "nodes":
		v.Nodes = renderNodes(w)
	case "deployments":
		v.Deployments = renderDeployments(w)
	case "costs":
		v.Costs = renderCosts(w)
	}
	return v
}
