package ocostsplitterplugin

import (
	"fmt"
	"hash/fnv"
	"time"
)

// CostRow is one FOCUS CustomCost row the plugin emits: the share of the
// shared pool allocated to one namespace for one window.
type CostRow struct {
	ID          string
	Namespace   string
	Description string
	BilledCost  float64
	Labels      map[string]string
}

// BuildRows turns split results into FOCUS CustomCost rows, one per
// attributed namespace, with the method and the shared sources in labels.
func BuildRows(windowStart, windowEnd time.Time, currency, method string,
	sharedSources map[string]float64, allocs map[string]float64) []CostRow {

	rows := []CostRow{}
	for ns, amount := range allocs {
		id := fmt.Sprintf("ocost-splitter-%s-%s-%d",
			method, ns, windowStart.UTC().Unix())
		desc := fmt.Sprintf(
			"Shared cost (control plane, ingress, monitoring, cross-namespace transfer) split by %s method. Shared pool: %.2f %s.",
			method, sharedPoolTotal(sharedSources), currency)

		labels := map[string]string{
			"split_method": method,
		}
		for src, share := range sharedSources {
			labels["shared_source_"+sanitizeLabelKey(src)] = fmt.Sprintf("%.4f", share)
		}

		rows = append(rows, CostRow{
			ID:          id,
			Namespace:   ns,
			Description: desc,
			BilledCost:  amount,
			Labels:      labels,
		})
	}
	return rows
}

func sharedPoolTotal(sources map[string]float64) float64 {
	total := 0.0
	for _, v := range sources {
		total += v
	}
	return total
}

// sanitizeLabelKey makes a namespace safe as a label key suffix.
func sanitizeLabelKey(ns string) string {
	out := make([]rune, 0, len(ns))
	for _, r := range ns {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			out = append(out, r)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}

// DeterministicID provides a stable, globally unique id for a cost row.
// Deterministic ids matter: the same window, namespace and method must
// always produce the same id, so re-runs are idempotent.
func DeterministicID(method, namespace string, windowStart time.Time) string {
	h := fnv.New64a()
	h.Write([]byte(fmt.Sprintf("%s|%s|%d", method, namespace, windowStart.UTC().Unix())))
	return fmt.Sprintf("ocost-splitter-%x", h.Sum64())
}
