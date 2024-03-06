package controller

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/cilium/ebpf/rlimit"
	"k8s.io/klog"

	"github.com/erda-project/ebpf-agent/metric"
	"github.com/erda-project/ebpf-agent/pkg/envconf"
	"github.com/erda-project/ebpf-agent/pkg/exporter/collector"
	"github.com/erda-project/erda-infra/base/servicehub"
)

type Config struct {
	Plugins []string `file:"plugins"`
}

type provider struct {
	Cfg *Config

	ctx             servicehub.Context
	plugins         []Plugin
	collectorClient *collector.ReportClient
}

func (p *provider) Init(ctx servicehub.Context) error {
	// Allow the current process to lock memory for eBPF resources.
	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatal(err)
	}
	p.ctx = ctx
	p.plugins = make([]Plugin, 0, len(p.Cfg.Plugins))
	reportConfig := &collector.CollectorConfig{}
	envconf.MustLoad(reportConfig)
	p.collectorClient = collector.CreateReportClient(reportConfig)
	return nil
}

func (p *provider) Run(ctx context.Context) error {
	for _, name := range p.Cfg.Plugins {
		plugin, err := findPlugin(p.ctx, name)
		if err != nil {
			return err
		}
		p.plugins = append(p.plugins, plugin)
	}
	ch := make(chan metric.Metric, 1000)
	for _, plugin := range p.plugins {
		go plugin.Gather(ch)
	}
	//influxAddr := os.Getenv("INFLUX_ADDR")
	//influxOrg := os.Getenv("INFLUX_ORG")
	//influxBucket := os.Getenv("INFLUX_BUCKET")
	//influxToken := os.Getenv("INFLUX_TOKEN")
	//influxdb := influxdb.NewInfluxdb(influxAddr, influxOrg, influxBucket, influxToken).Run()
	for {
		select {
		case m := <-ch:
			//处理metric, print / influxdb / prometheus / erda   等
			//klog.Infof("[%d] metric is waiting to write", len(ch))
			// TODO： push other metrics to collector
			if m.Name == "docker_container_summary" || strings.HasPrefix(m.Name, "application_") {
				if err := p.collectorClient.Send([]*metric.Metric{&m}); err != nil {
					klog.Errorf("send metric to collector error: %v", err)
					continue
				}
				//klog.Infof("send metric to collector success")
			}
			//influxdb.Write(m)
		}
	}
	return nil
}

func (p *provider) Close() error {
	return nil
}

func findPlugin(ctx servicehub.Context, name string) (Plugin, error) {
	obj := ctx.Service(name)
	if obj == nil {
		return nil, fmt.Errorf("plugin %s not found", name)
	}
	plugin, ok := obj.(Plugin)
	if !ok {
		return nil, fmt.Errorf("item %s is not plugin", name)
	}
	return plugin, nil
}

func init() {
	servicehub.Register("agent.controller", &servicehub.Spec{
		Services: []string{},
		ConfigFunc: func() interface{} {
			return &Config{}
		},
		Creator: func() servicehub.Provider {
			return &provider{}
		},
	})
}
