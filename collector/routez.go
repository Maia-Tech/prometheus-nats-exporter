// Copyright 2017-2025 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package collector has various collector utilities and implementations.
package collector

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	routezEndpoint         = "routez"
	routezDetailedEndpoint = "routez_detailed"
)

func isRoutezEndpoint(system, endpoint string) bool {
	return system == CoreSystem && (endpoint == routezEndpoint || endpoint == routezDetailedEndpoint)
}

type routezCollector struct {
	sync.Mutex

	httpClient *http.Client
	servers    []*CollectedServer
	detailed   bool

	numRoutes *prometheus.Desc
	routezCollectorDetailed
}

type routezCollectorDetailed struct {
	routeRtt           *prometheus.Desc
	routeUptime        *prometheus.Desc
	routeIdle          *prometheus.Desc
	routeInMsgs        *prometheus.Desc
	routeOutMsgs       *prometheus.Desc
	routeInBytes       *prometheus.Desc
	routeOutBytes      *prometheus.Desc
	routeSubscriptions *prometheus.Desc
	routePendingSize   *prometheus.Desc
}

func createRoutezCollector(system string) *routezCollector {
	summaryLabels := []string{"server_id"}
	return &routezCollector{
		httpClient: http.DefaultClient,
		numRoutes: prometheus.NewDesc(
			prometheus.BuildFQName(system, routezEndpoint, "num_routes"),
			"num_routes",
			summaryLabels,
			nil,
		),
	}
}

func createRoutezDetailedCollector(system string) *routezCollector {
	routezCollector := createRoutezCollector(system)
	detailLabels := []string{"server_id", "server_name", "remote_id", "remote_name", "rid"}
	routezCollector.routeRtt = prometheus.NewDesc(
		prometheus.BuildFQName(system, routezEndpoint, "route_rtt"),
		"Round-trip time in seconds",
		detailLabels,
		nil,
	)
	routezCollector.routeUptime = prometheus.NewDesc(
		prometheus.BuildFQName(system, routezEndpoint, "route_uptime"),
		"Route uptime in seconds",
		detailLabels,
		nil,
	)
	routezCollector.routeIdle = prometheus.NewDesc(
		prometheus.BuildFQName(system, routezEndpoint, "route_idle"),
		"Idle time in seconds",
		detailLabels,
		nil,
	)
	routezCollector.routeInMsgs = prometheus.NewDesc(
		prometheus.BuildFQName(system, routezEndpoint, "route_in_msgs"),
		"Total messages received",
		detailLabels,
		nil,
	)
	routezCollector.routeOutMsgs = prometheus.NewDesc(
		prometheus.BuildFQName(system, routezEndpoint, "route_out_msgs"),
		"Total messages sent",
		detailLabels,
		nil,
	)
	routezCollector.routeInBytes = prometheus.NewDesc(
		prometheus.BuildFQName(system, routezEndpoint, "route_in_bytes"),
		"Total bytes received",
		detailLabels,
		nil,
	)
	routezCollector.routeOutBytes = prometheus.NewDesc(
		prometheus.BuildFQName(system, routezEndpoint, "route_out_bytes"),
		"Total bytes sent",
		detailLabels,
		nil,
	)
	routezCollector.routeSubscriptions = prometheus.NewDesc(
		prometheus.BuildFQName(system, routezEndpoint, "route_subscriptions"),
		"Number of subscriptions on this route",
		detailLabels,
		nil,
	)
	routezCollector.routePendingSize = prometheus.NewDesc(
		prometheus.BuildFQName(system, routezEndpoint, "route_pending_size"),
		"Pending buffer size in bytes",
		detailLabels,
		nil,
	)
	return routezCollector
}

func newRoutezCollector(system, endpoint string, servers []*CollectedServer) prometheus.Collector {
	var nc *routezCollector
	if endpoint == routezDetailedEndpoint {
		nc = createRoutezDetailedCollector(system)
		nc.detailed = true
	} else {
		nc = createRoutezCollector(system)
	}
	nc.servers = make([]*CollectedServer, len(servers))
	for i, s := range servers {
		nc.servers[i] = &CollectedServer{
			ID:  s.ID,
			URL: s.URL + "/" + routezEndpoint,
		}
	}
	return nc
}

func (nc *routezCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- nc.numRoutes
	if nc.detailed {
		ch <- nc.routeRtt
		ch <- nc.routeUptime
		ch <- nc.routeIdle
		ch <- nc.routeInMsgs
		ch <- nc.routeOutMsgs
		ch <- nc.routeInBytes
		ch <- nc.routeOutBytes
		ch <- nc.routeSubscriptions
		ch <- nc.routePendingSize
	}
}

// Collect gathers the server routez metrics.
func (nc *routezCollector) Collect(ch chan<- prometheus.Metric) {
	for _, server := range nc.servers {
		var resp Routez
		if err := getMetricURL(nc.httpClient, server.URL, &resp); err != nil {
			Debugf("ignoring server %s: %v", server.ID, err)
			continue
		}

		if nc.detailed {
			for _, route := range resp.Routes {
				detailLabelValues := []string{
					server.ID,
					resp.ServerName,
					route.RemoteID,
					route.RemoteName,
					fmt.Sprint(route.RID),
				}

				// Parse RTT - convert microseconds string to seconds
				rtt, err := time.ParseDuration(route.RTT)
				if err != nil {
					Debugf("failed to parse rtt %s: %v", route.RTT, err)
					rtt = 0
				}
				ch <- prometheus.MustNewConstMetric(nc.routeRtt, prometheus.GaugeValue,
					rtt.Seconds(), detailLabelValues...)

				// Parse Uptime - convert to seconds
				uptime := parseDuration(route.Uptime) / 1000 // parseDuration returns milliseconds
				ch <- prometheus.MustNewConstMetric(nc.routeUptime, prometheus.GaugeValue,
					uptime, detailLabelValues...)

				// Parse Idle - convert to seconds
				idle := parseDuration(route.Idle) / 1000 // parseDuration returns milliseconds
				ch <- prometheus.MustNewConstMetric(nc.routeIdle, prometheus.GaugeValue,
					idle, detailLabelValues...)

				ch <- prometheus.MustNewConstMetric(nc.routeInMsgs, prometheus.CounterValue,
					float64(route.InMsgs), detailLabelValues...)

				ch <- prometheus.MustNewConstMetric(nc.routeOutMsgs, prometheus.CounterValue,
					float64(route.OutMsgs), detailLabelValues...)

				ch <- prometheus.MustNewConstMetric(nc.routeInBytes, prometheus.CounterValue,
					float64(route.InBytes), detailLabelValues...)

				ch <- prometheus.MustNewConstMetric(nc.routeOutBytes, prometheus.CounterValue,
					float64(route.OutBytes), detailLabelValues...)

				ch <- prometheus.MustNewConstMetric(nc.routeSubscriptions, prometheus.GaugeValue,
					float64(route.Subscriptions), detailLabelValues...)

				ch <- prometheus.MustNewConstMetric(nc.routePendingSize, prometheus.GaugeValue,
					float64(route.PendingSize), detailLabelValues...)
			}
		}

		ch <- prometheus.MustNewConstMetric(nc.numRoutes, prometheus.GaugeValue,
			float64(resp.NumRoutes), server.ID)
	}
}

// Routez output
type Routez struct {
	ServerID   string   `json:"server_id"`
	ServerName string   `json:"server_name"`
	Now        string   `json:"now"`
	NumRoutes  int      `json:"num_routes"`
	Routes     []*Route `json:"routes"`
}

// Route output
type Route struct {
	RID           int    `json:"rid"`
	RemoteID      string `json:"remote_id"`
	RemoteName    string `json:"remote_name"`
	DidSolicit    bool   `json:"did_solicit"`
	IsConfigured  bool   `json:"is_configured"`
	IP            string `json:"ip"`
	Port          int    `json:"port"`
	Start         string `json:"start"`
	LastActivity  string `json:"last_activity"`
	RTT           string `json:"rtt"`
	Uptime        string `json:"uptime"`
	Idle          string `json:"idle"`
	PendingSize   int    `json:"pending_size"`
	InMsgs        int    `json:"in_msgs"`
	OutMsgs       int    `json:"out_msgs"`
	InBytes       int    `json:"in_bytes"`
	OutBytes      int    `json:"out_bytes"`
	Subscriptions int    `json:"subscriptions"`
	Compression   string `json:"compression"`
}
