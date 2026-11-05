package observability_test

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func labelsMatch(m *dto.Metric, want map[string]string) bool {
	if len(m.GetLabel()) != len(want) {
		return false
	}
	for _, lp := range m.GetLabel() {
		if want[lp.GetName()] != lp.GetValue() {
			return false
		}
	}
	return true
}

func parseSelector(sel string) (string, map[string]string) {
	name, rest, _ := strings.Cut(sel, "{")
	labels := map[string]string{}
	rest = strings.TrimSuffix(rest, "}")
	for _, kv := range strings.Split(rest, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			labels[k] = strings.Trim(v, `"`)
		}
	}
	return name, labels
}

func gatherValue(t *testing.T, reg *prometheus.Registry, sel string) float64 {
	t.Helper()
	name, labels := parseSelector(sel)
	fams, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fams {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			if labelsMatch(m, labels) {
				return m.GetCounter().GetValue()
			}
		}
	}
	t.Fatalf("series %s not found", sel)
	return 0
}

func findHist(t *testing.T, reg *prometheus.Registry, name string, kv ...string) *dto.Histogram {
	t.Helper()
	want := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		want[kv[i]] = kv[i+1]
	}
	fams, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fams {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			if len(want) == 0 || labelsMatch(m, want) {
				return m.GetHistogram()
			}
		}
	}
	t.Fatalf("histogram %s not found", name)
	return nil
}

func histCount(t *testing.T, reg *prometheus.Registry, name string, kv ...string) uint64 {
	t.Helper()
	return findHist(t, reg, name, kv...).GetSampleCount()
}

func histSum(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	return findHist(t, reg, name).GetSampleSum()
}
