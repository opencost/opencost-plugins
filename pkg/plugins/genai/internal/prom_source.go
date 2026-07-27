package internal

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// HttpPrometheusQuerier implements PrometheusQuerier using the standard client.
type HttpPrometheusQuerier struct {
	api v1.API
}

func NewHttpPrometheusQuerier(url string) (*HttpPrometheusQuerier, error) {
	client, err := api.NewClient(api.Config{Address: url})
	if err != nil {
		return nil, err
	}
	return &HttpPrometheusQuerier{api: v1.NewAPI(client)}, nil
}

func (q *HttpPrometheusQuerier) Query(ctx context.Context, query string) (interface{}, error) {
	val, _, err := q.api.Query(ctx, query, time.Now())
	return val, err
}

// This allows decoupling from the prometheus-source module
type PrometheusQuerier interface {
	Query(ctx context.Context, query string) (interface{}, error)
}

type PrometheusProvider struct {
	querier PrometheusQuerier
}

func NewPrometheusProvider(querier PrometheusQuerier) *PrometheusProvider {
	return &PrometheusProvider{querier: querier}
}

// 1. Logic for Counter vs Gauge PromQL
func (p *PrometheusProvider) buildQuery(metric string, window time.Duration, isCounter bool, mapping MetricMapping) string {
	windowStr := fmt.Sprintf("%.0fs", window.Seconds())
	labels := fmt.Sprintf("%s, %s, %s", mapping.ClusterLabel, mapping.NamespaceLabel, mapping.PodLabel)
	if isCounter {
		// Delta increase for counters (tokens, gpu active seconds)
		return fmt.Sprintf("sum(increase(%s[%s])) by (%s)", metric, windowStr, labels)
	}
	// Average for gauges (gpu utilization)
	return fmt.Sprintf("avg(avg_over_time(%s[%s])) by (%s)", metric, windowStr, labels)
}

// 2. Simple execution wrapper
func (p *PrometheusProvider) execute(ctx context.Context, query string) (model.Vector, error) {
	// Use Context-aware querying
	val, err := p.querier.Query(ctx, query)
	if err != nil {
		return nil, err
	}

	switch v := val.(type) {
	case model.Vector:
		return v, nil
	case *model.Vector:
		if v == nil {
			return nil, fmt.Errorf("prometheus returned nil vector")
		}
		return *v, nil
	default:
		return nil, fmt.Errorf("unexpected prometheus result type: %T", val)
	}
}

// 3. Mapping logic for standardizing output
func (p *PrometheusProvider) mapResults(vector model.Vector, results map[string]*Metrics, key string, mapping MetricMapping, defaultClusterID string) {
	for _, sample := range vector {
		pod := string(sample.Metric[model.LabelName(mapping.PodLabel)])
		ns := string(sample.Metric[model.LabelName(mapping.NamespaceLabel)])
		cluster := string(sample.Metric[model.LabelName(mapping.ClusterLabel)])
		if pod == "" || ns == "" {
			continue
		}

		if cluster == "" {
			cluster = defaultClusterID
		}

		id := fmt.Sprintf("%s/%s/%s", cluster, ns, pod)
		if _, ok := results[id]; !ok {
			results[id] = &Metrics{
				Cluster:       cluster,
				Pod:           pod,
				Namespace:     ns,
				ModelName:     string(sample.Metric[model.LabelName(mapping.ModelLabel)]),
				WorkflowPhase: string(sample.Metric[model.LabelName(mapping.WorkflowLabel)]),
			}
		}

		val := float64(sample.Value)
		switch key {
		case "input":
			results[id].InputTokens = val
		case "output":
			results[id].OutputTokens = val
		case "gpuSec":
			results[id].GPUActiveSec = val
		case "util":
			results[id].GPUUtilPercent = val
		}
	}
}

// 4. Batch orchestrator for the 4-query sequence
func (p *PrometheusProvider) Fetch(ctx context.Context, start, end time.Time, mapping MetricMapping, defaultClusterID string) (map[string]*Metrics, error) {
	window := end.Sub(start)
	results := make(map[string]*Metrics)

	steps := []struct {
		id      string
		metric  string
		counter bool
	}{
		{"input", mapping.InputTokens, true},
		{"output", mapping.OutputTokens, true},
		{"gpuSec", mapping.GPUActiveSec, true},
		{"util", mapping.GPUUtilization, false},
	}

	for _, s := range steps {
		q := p.buildQuery(s.metric, window, s.counter, mapping)
		v, err := p.execute(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("fetch error for %s: %w", s.id, err)
		}
		p.mapResults(v, results, s.id, mapping, defaultClusterID)
	}

	return results, nil
}

// FetchMIGRequests fetches pod-level MIG requests.
func (p *PrometheusProvider) FetchMIGRequests(ctx context.Context) (map[string]map[string]float64, error) {
	query := `kube_pod_container_resource_requests{resource=~"nvidia.com/mig-.*"}`
	val, err := p.execute(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("mig requests error: %w", err)
	}

	results := make(map[string]map[string]float64)
	for _, sample := range val {
		pod := string(sample.Metric["pod"])
		ns := string(sample.Metric["namespace"])
		resource := string(sample.Metric["resource"])
		if pod == "" || ns == "" || resource == "" {
			continue
		}

		id := fmt.Sprintf("%s/%s", ns, pod)
		if _, ok := results[id]; !ok {
			results[id] = make(map[string]float64)
		}
		results[id][resource] = float64(sample.Value)
	}
	return results, nil
}

// FetchNodeMIGCapacity fetches total MIG capacities across nodes.
func (p *PrometheusProvider) FetchNodeMIGCapacity(ctx context.Context) (NodeCapacityMap, error) {
	query := `kube_node_status_capacity{resource=~"nvidia.com/mig-.*"}`
	val, err := p.execute(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("node mig capacity error: %w", err)
	}

	results := make(NodeCapacityMap)
	for _, sample := range val {
		resource := string(sample.Metric["resource"])
		if resource == "" {
			continue
		}
		results[resource] += float64(sample.Value)
	}
	return results, nil
}
