package ocostsplitterplugin

import "fmt"

// Split methods for the shared/unattributed pool.
const (
	MethodProportional = "proportional"
	MethodEqual        = "equal"
	MethodWeighted     = "weighted"
	MethodNone         = "none"
)

// ValidMethods lists the written, auditable split methods.
var ValidMethods = []string{MethodProportional, MethodEqual, MethodWeighted, MethodNone}

// IsValidMethod reports whether the given method is one of the four written methods.
func IsValidMethod(method string) bool {
	for _, m := range ValidMethods {
		if m == method {
			return true
		}
	}
	return false
}

// Target is one namespace that can receive a share of the shared pool.
type Target struct {
	Namespace string
	Weight    float64
}

// SplitShared distributes the shared pool across targets using the selected
// method. The returned map is namespace -> allocated cost. The sum of the
// returned values equals pool (within float rounding).
func SplitShared(pool float64, targets []Target, method string) (map[string]float64, error) {
	if pool < 0 {
		return nil, fmt.Errorf("shared pool cannot be negative: %f", pool)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no targets to split the shared pool across")
	}

	out := map[string]float64{}

	switch method {
	case MethodNone:
		// Baseline: the pool stays unsplit. One row carries it, named shared.
		out["shared"] = pool
		return out, nil

	case MethodEqual:
		share := pool / float64(len(targets))
		for _, t := range targets {
			out[t.Namespace] = share
		}
		return out, nil

	case MethodProportional, MethodWeighted:
		total := 0.0
		for _, t := range targets {
			if t.Weight < 0 {
				return nil, fmt.Errorf("negative weight for namespace %s: %f", t.Namespace, t.Weight)
			}
			total += t.Weight
		}
		if total <= 0 {
			return nil, fmt.Errorf("total weight is %f; proportional and weighted methods need a positive total", total)
		}
		for _, t := range targets {
			out[t.Namespace] = pool * t.Weight / total
		}
		return out, nil

	default:
		return nil, fmt.Errorf("unknown split method %q; valid methods: %v", method, ValidMethods)
	}
}
