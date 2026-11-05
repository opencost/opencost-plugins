package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonconfig "github.com/opencost/opencost-plugins/pkg/common/config"
	ocostsplitterplugin "github.com/opencost/opencost-plugins/pkg/plugins/ocost-splitter/ocostsplitterplugin"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/opencost/opencost/core/pkg/opencost"
	ocplugin "github.com/opencost/opencost/core/pkg/plugin"
)

// handshakeConfig is used to do a basic handshake between the plugin and
// the OpenCost host. A failure here produces a friendly error instead of
// a silent hang.
var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PLUGIN_NAME",
	MagicCookieValue: "ocost-splitter",
}

// OcostSplitterSource implements the CustomCostSource interface.
// It reads the shared/unattributed pool from the OpenCost allocation API,
// splits it across the namespaces that ran work in the window using one of
// four written methods, and returns FOCUS rows OpenCost can ingest.
type OcostSplitterSource struct {
	Config ocostsplitterplugin.Config
	Client *http.Client
}

// NewOcostSplitterSource builds the source from a parsed config with
// family defaults applied.
func NewOcostSplitterSource(cfg ocostsplitterplugin.Config) *OcostSplitterSource {
	if cfg.OpencostAPIAddress == "" {
		cfg.OpencostAPIAddress = "http://opencost.opencost:9003"
	}
	if cfg.SplitMethod == "" {
		cfg.SplitMethod = ocostsplitterplugin.MethodProportional
	}
	if len(cfg.SharedNamespaces) == 0 {
		cfg.SharedNamespaces = ocostsplitterplugin.DefaultSharedNamespaces
	}
	if cfg.Currency == "" {
		cfg.Currency = "USD"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	return &OcostSplitterSource{
		Config: cfg,
		Client: &http.Client{Timeout: 60 * time.Second},
	}
}

// GetCustomCosts implements the CustomCostSource interface. OpenCost calls
// this with a window and resolution; the plugin returns FOCUS rows with the
// shared pool split per namespace.
func (s *OcostSplitterSource) GetCustomCosts(req *pb.CustomCostRequest) []*pb.CustomCostResponse {
	results := []*pb.CustomCostResponse{}

	targets, err := opencost.GetWindows(req.Start.AsTime(), req.End.AsTime(), req.Resolution.AsDuration())
	if err != nil {
		log.Errorf("error getting windows: %v", err)
		results = append(results, errorResponse(err))
		return results
	}

	for _, w := range targets {
		if w.Start().After(time.Now().UTC()) {
			log.Debugf("skipping future window %v", w)
			continue
		}
		results = append(results, s.getSplitCostsForWindow(w))
	}
	return results
}

func (s *OcostSplitterSource) getSplitCostsForWindow(w opencost.Window) *pb.CustomCostResponse {
	ccResp := boilerplateResponse(w)

	rows, err := ocostsplitterplugin.FetchAllocations(
		s.Config.OpencostAPIAddress, *w.Start(), *w.End(), s.Client)
	if err != nil {
		log.Errorf("error fetching allocations: %v", err)
		ccResp.Errors = append(ccResp.Errors, err.Error())
		return &ccResp
	}

	// Split the pool: shared namespaces' totalCost becomes the pool;
	// remaining namespaces are the targets. Proportional weight is each
	// namespace's own totalCost - the scheduler-reserved share.
	pool := 0.0
	targets := []ocostsplitterplugin.Target{}
	sources := map[string]float64{}
	for _, row := range rows {
		if s.Config.IsShared(row.Namespace) {
			pool += row.TotalCost
			sources[row.Namespace] = row.TotalCost
		} else if row.TotalCost > 0 {
			targets = append(targets, ocostsplitterplugin.Target{
				Namespace: row.Namespace,
				Weight:    row.TotalCost,
			})
		}
	}

	if pool <= 0 {
		log.Debugf("no shared pool for window %v; nothing to split", w)
		return &ccResp
	}
	if len(targets) == 0 {
		ccResp.Errors = append(ccResp.Errors,
			"no attributed namespaces found; cannot split the shared pool")
		return &ccResp
	}

	// Weighted method: load weights from the agreed file.
	method := s.Config.SplitMethod
	weightTargets := targets
	if method == ocostsplitterplugin.MethodWeighted {
		weights, err := ocostsplitterplugin.LoadWeights(s.Config.WeightsFile)
		if err != nil {
			ccResp.Errors = append(ccResp.Errors, err.Error())
			return &ccResp
		}
		weightTargets = targets
		for i := range weightTargets {
			w, found := weights[weightTargets[i].Namespace]
			if !found || w <= 0 {
				log.Warnf("no weight for namespace %s; excluding from split", weightTargets[i].Namespace)
				weightTargets[i].Weight = 0
				continue
			}
			weightTargets[i].Weight = w
		}
	}

	allocs, err := ocostsplitterplugin.SplitShared(pool, weightTargets, method)
	if err != nil {
		ccResp.Errors = append(ccResp.Errors, err.Error())
		return &ccResp
	}

	rowsOut := ocostsplitterplugin.BuildRows(
		*w.Start(), *w.End(), s.Config.Currency, method, sources, allocs)

	for _, row := range rowsOut {
		ccResp.Costs = append(ccResp.Costs, &pb.CustomCost{
			Zone:           "",
			AccountName:    "cluster",
			ChargeCategory: "usage",
			Description:    row.Description,
			ResourceName:   row.Namespace,
			ResourceType:   "kubernetes-namespace",
			Id:             row.ID,
			ProviderId:     fmt.Sprintf("ocost-splitter/%s", row.Namespace),
			Labels:         row.Labels,
			BilledCost:     float32(row.BilledCost),
		})
	}

	log.Debugf("split %f across %d namespaces using %s", pool, len(allocs), method)
	return &ccResp
}

func boilerplateResponse(w opencost.Window) pb.CustomCostResponse {
	return pb.CustomCostResponse{
		Metadata:   map[string]string{"split_rule": "written and signed"},
		CostSource: "finops",
		Domain:     "ocost-splitter",
		Version:    "v1",
		Currency:   "USD",
		Start:      timestamppb.New(*w.Start()),
		End:        timestamppb.New(*w.End()),
		Errors:     []string{},
		Costs:      []*pb.CustomCost{},
	}
}

func errorResponse(err error) *pb.CustomCostResponse {
	return &pb.CustomCostResponse{
		CostSource: "finops",
		Domain:     "ocost-splitter",
		Version:    "v1",
		Errors:     []string{err.Error()},
		Costs:      []*pb.CustomCost{},
	}
}

func main() {
	configFile, err := commonconfig.GetConfigFilePath()
	if err != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	cfgBytes, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatalf("error reading config file: %v", err)
	}

	var cfg ocostsplitterplugin.Config
	if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
		log.Fatalf("error parsing config: %v", err)
	}

	source := NewOcostSplitterSource(cfg)

	var pluginMap = map[string]plugin.Plugin{
		"CustomCostSource": &ocplugin.CustomCostPlugin{Impl: source},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}
