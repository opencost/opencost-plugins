package internal

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

type Scraper struct {
	Client  *http.Client
	Timeout time.Duration
}

func NewScraper(timeout time.Duration) *Scraper {
	return &Scraper{
		Client:  &http.Client{Timeout: timeout},
		Timeout: timeout,
	}
}

func (s *Scraper) fetchRawMetrics(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("scrape failed: status %d", resp.StatusCode)
	}

	return resp.Body, nil
}

// Updated to use dto.MetricFamily
func (s *Scraper) parseToFamilies(data io.Reader) (map[string]*dto.MetricFamily, error) {
	var parser expfmt.TextParser
	return parser.TextToMetricFamilies(data)
}

// Updated mapping logic for the DTO structs to extract dimensions
func (s *Scraper) extractMetricsByLabels(families map[string]*dto.MetricFamily, mappedName string, mapping MetricMapping, results map[string]*Metrics, metricKey string) {
	family, ok := families[mappedName]
	if !ok || family == nil {
		return
	}

	for _, m := range family.Metric {
		var val float64
		if m.Counter != nil && m.Counter.Value != nil {
			val = *m.Counter.Value
		} else if m.Gauge != nil && m.Gauge.Value != nil {
			val = *m.Gauge.Value
		} else if m.Untyped != nil && m.Untyped.Value != nil {
			val = *m.Untyped.Value
		}

		labels := make(map[string]string)
		for _, lp := range m.Label {
			if lp.Name != nil && lp.Value != nil {
				labels[*lp.Name] = *lp.Value
			}
		}

		cluster := labels[mapping.ClusterLabel]
		pod := labels[mapping.PodLabel]
		ns := labels[mapping.NamespaceLabel]
		modelName := labels[mapping.ModelLabel]
		workflowPhase := labels[mapping.WorkflowLabel]

		key := fmt.Sprintf("%s/%s/%s/%s/%s", cluster, ns, pod, modelName, workflowPhase)
		if _, ok := results[key]; !ok {
			results[key] = &Metrics{
				Cluster:       cluster,
				Namespace:     ns,
				Pod:           pod,
				ModelName:     modelName,
				WorkflowPhase: workflowPhase,
			}
		}

		switch metricKey {
		case "input":
			results[key].InputTokens += val
		case "output":
			results[key].OutputTokens += val
		case "gpuSec":
			results[key].GPUActiveSec += val
		case "util":
			results[key].GPUUtilPercent += val
		}
	}
}

func (s *Scraper) Scrape(ctx context.Context, ip, port string, mapping MetricMapping) ([]*Metrics, error) {
	url := fmt.Sprintf("http://%s:%s/metrics", ip, port)

	raw, err := s.fetchRawMetrics(ctx, url)
	if err != nil {
		return nil, err
	}
	defer raw.Close()

	families, err := s.parseToFamilies(raw)
	if err != nil {
		return nil, err
	}

	results := make(map[string]*Metrics)
	s.extractMetricsByLabels(families, mapping.InputTokens, mapping, results, "input")
	s.extractMetricsByLabels(families, mapping.OutputTokens, mapping, results, "output")
	s.extractMetricsByLabels(families, mapping.GPUActiveSec, mapping, results, "gpuSec")
	s.extractMetricsByLabels(families, mapping.GPUUtilization, mapping, results, "util")

	var ret []*Metrics
	for _, m := range results {
		ret = append(ret, m)
	}

	return ret, nil
}
