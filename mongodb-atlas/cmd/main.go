package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/opencost/opencost-plugins/mongodb-atlas/plugin"
	"github.com/opencost/opencost/core/pkg/log"
)

func main() {
	fmt.Println("Initialize plugin")

}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// pass list of orgs , start date, end date
func createCostExplorerQueryToken(org string, startDate time.Time, endDate time.Time,
	client HTTPClient, url string) (string, error) {
	// Define the layout for the desired format
	layout := "2006-01-02"

	// Convert the time.Time object to a string in yyyy-mm-dd format
	startDateString := startDate.Format(layout)
	endDateString := endDate.Format(layout)

	payload := plugin.CreateCostExplorerQueryPayload{

		EndDate:       endDateString,
		StartDate:     startDateString,
		Organizations: []string{org},
	}
	payloadJson, _ := json.Marshal(payload)

	request, _ := http.NewRequest("POST", url, bytes.NewBuffer(payloadJson))

	request.Header.Set("Accept", "application/vnd.atlas.2023-01-01+json")
	request.Header.Set("Content-Type", "application/vnd.atlas.2023-01-01+json")

	response, error := client.Do(request)
	if error != nil {
		msg := fmt.Sprintf("createCostExplorerQueryToken: error from server: %v", error)
		log.Errorf(msg)
		return "", fmt.Errorf(msg)

	}
	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)
	//fmt.Println("response Body:", string(body))
	var createCostExplorerQueryResponse plugin.CreateCostExplorerQueryResponse
	respUnmarshalError := json.Unmarshal([]byte(body), &createCostExplorerQueryResponse)
	if respUnmarshalError != nil {
		msg := fmt.Sprintf("createCostExplorerQueryToken: error unmarshalling response: %v", respUnmarshalError)
		log.Errorf(msg)
		return "", fmt.Errorf(msg)
	}
	return createCostExplorerQueryResponse.Token, nil
}
