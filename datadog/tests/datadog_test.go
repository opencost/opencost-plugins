package tests

import (
	"os"
	"testing"

	"github.com/opencost/opencost/core/pkg/log"
)

// this test gets DD keys from env vars, writes a config file
// then it invokes the plugin and validates some basics
// in the response
func TestDDCostRetrieval(t *testing.T) {
	// read necessary env vars. If any are missing, log warning and skip test
	ddSite := os.Getenv("DD_SITE")
	ddApiKey := os.Getenv("DD_API_KEY")
	ddAppKey := os.Getenv("DD_APPLICATION_KEY")

	if ddSite == "" {
		log.Warnf("DD_SITE undefined, this needs to have the URL of your DD instance, skipping test")
		t.Skip()
		return
	}

	if ddApiKey == "" {
		log.Warnf("DD_API_KEY undefined, skipping test")
		t.Skip()
		return
	}

	if ddAppKey == "" {
		log.Warnf("DD_APPLICATION_KEY undefined, skipping test")
		t.Skip()
		return
	}
	
	// write out config to temp file using contents of env vars

	// set up custom cost request

	// invoke plugin via harness

	// confirm no errors in result

	// confirm results have correct provider

	// confirm there are > 0 custom costs
	// confirm each custom cost has
}
