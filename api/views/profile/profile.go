package profile

import (
	"context"
	"fmt"
	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/profiling"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
	"k8s.io/klog"
	"net/url"
	"sort"
	"strings"
)

type View struct {
	Status       model.Status       `json:"status"`
	Message      string             `json:"message"`
	Applications []Application      `json:"applications"`
	Profiles     []Meta             `json:"profiles"`
	Profile      *profiling.Profile `json:"profile"`
	Chart        *model.Chart       `json:"chart"`
}

type Application struct {
	Name   string `json:"name"`
	Linked bool   `json:"linked"`
}

type Meta struct {
	Type     profiling.Type `json:"type"`
	Name     string         `json:"name"`
	Selected bool           `json:"selected"`
}

func Render(ctx context.Context, pyroscope *profiling.PyroscopeClient, app *model.Application, appSettings *db.ApplicationSettings, q url.Values, wCtx timeseries.Context) *View {
	if pyroscope == nil {
		return nil
	}

	parts := strings.Split(q.Get("profile")+"::::", ":")
	typ, name, view, sFrom, sTo := parts[0], parts[1], parts[2], parts[3], parts[4]
	from := utils.ParseTime(wCtx.To, sFrom, wCtx.From)
	to := utils.ParseTime(wCtx.To, sTo, wCtx.To)

	v := &View{}

	meta, err := pyroscope.Metadata(ctx)
	if err != nil {
		klog.Errorln(err)
		v.Status = model.WARNING
		v.Message = fmt.Sprintf("pyroscope error: %s", err)
		return v
	}

	pyroscopeApplication := app.Id.Name
	disabled := false
	if appSettings != nil && appSettings.Pyroscope != nil {
		pyroscopeApplication = appSettings.Pyroscope.Application
		disabled = appSettings.Pyroscope.Application == ""
	}
	apps := meta.GetApplications()
	for a := range apps {
		if a == "" {
			continue
		}
		v.Applications = append(v.Applications, Application{
			Name:   a,
			Linked: !disabled && a == pyroscopeApplication,
		})
	}
	sort.Slice(v.Applications, func(i, j int) bool {
		return v.Applications[i].Name < v.Applications[j].Name
	})

	appProfiles := apps[pyroscopeApplication]
	if disabled {
		appProfiles = nil
	}
	ebpfProfiles := apps[""]

	profiles := append(appProfiles, ebpfProfiles...)
	sort.Slice(profiles, func(i, j int) bool {
		pi, pj := profiles[i], profiles[j]
		if pi.Type == pj.Type {
			return pi.Name < pj.Name
		}
		return pi.Type < pj.Type
	})

	profile := match(profiles, profiling.Type(typ), name)
	for _, p := range profiles {
		v.Profiles = append(v.Profiles, Meta{Type: p.Type, Name: p.Name, Selected: p == profile})
	}
	switch {
	case profile == nil:
		v.Status = model.UNKNOWN
		v.Message = "No profiles found"
		return v
	case profile.Spy == profiling.SpyAgent:
		v.Status = model.OK
		v.Message = "Using data gathered by the eBPF profiler"
	default:
		v.Status = model.OK
		v.Message = fmt.Sprintf("Using profiles of <i>%s@pyroscope</i>", pyroscopeApplication)
	}

	v.Profile, err = pyroscope.Profile(ctx, profiling.View(view), getQuery(app, profile), from, to)
	if err != nil {
		klog.Errorln(err)
		v.Status = model.WARNING
		v.Message = fmt.Sprintf("pyroscope error: %s", err)
		return v
	}

	cpuChart, memoryChart := getCharts(app, wCtx)
	switch profile.Type {
	case profiling.TypeCPU:
		v.Chart = cpuChart
	case profiling.TypeMemory:
		v.Chart = memoryChart
	}

	return v
}

func match(profiles []*profiling.ProfileMeta, typ profiling.Type, name string) *profiling.ProfileMeta {
	var matched []*profiling.ProfileMeta
	switch {
	case typ != "" && name != "":
		for _, p := range profiles {
			if p.Type == typ && p.Name == name {
				matched = append(matched, p)
			}
		}
	case typ != "":
		for _, p := range profiles {
			if p.Type == typ {
				matched = append(matched, p)
			}
		}
	default:
		matched = profiles
	}

	if len(matched) == 0 {
		return nil
	}

	for _, f := range featured {
		for _, p := range matched {
			if p.Type == f.typ && p.Name == f.name {
				return p
			}
		}
	}
	return matched[0]
}

func getQuery(app *model.Application, profile *profiling.ProfileMeta) string {
	query := profile.Query
	if profile.Spy != profiling.SpyAgent {
		return query
	}
	services := utils.NewStringSet()
	containers := utils.NewStringSet()
	for _, i := range app.Instances {
		for _, c := range i.Containers {
			services.Add(model.ContainerIdToServiceName(c.Id))
			containers.Add(c.Id)
		}
	}
	query += fmt.Sprintf(
		`{service_name=~"(%s)", container_id=~"(%s)"}`,
		strings.Join(services.Items(), "|"),
		strings.Join(containers.Items(), "|"),
	)
	return query
}

func getCharts(app *model.Application, ctx timeseries.Context) (*model.Chart, *model.Chart) {
	events := model.EventsToAnnotations(app.Events, ctx)
	incidents := model.IncidentsToAnnotations(app.Incidents, ctx)
	cpuChart := model.NewChart(ctx, "CPU usage by instance, cores").Stacked().AddAnnotation(events...).AddAnnotation(incidents...)
	memoryChart := model.NewChart(ctx, "Memory (RSS) usage by instance, bytes").Stacked().AddAnnotation(events...).AddAnnotation(incidents...)
	for _, i := range app.Instances {
		cpu := timeseries.NewAggregate(timeseries.NanSum)
		rss := timeseries.NewAggregate(timeseries.NanSum)
		for _, c := range i.Containers {
			cpu.Add(c.CpuUsage)
			rss.Add(c.MemoryRss)
		}
		cpuChart.AddSeries(i.Name, cpu)
		memoryChart.AddSeries(i.Name, rss)
	}
	return cpuChart, memoryChart
}

var featured = []struct {
	typ  profiling.Type
	name string
}{
	{typ: profiling.TypeCPU, name: ""},
	{typ: profiling.TypeCPU, name: "itimer"},
	{typ: profiling.TypeMemory, name: "inuse_space"},
}
