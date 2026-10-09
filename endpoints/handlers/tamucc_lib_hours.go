package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// The library homepage (tamucc.edu/library) fills #todays-hours-hours from
// this LibCal feed using locations[0].rendered, so we read it directly
// instead of scraping the page with a headless browser.
const tamuccLibHoursURL = "https://api3.libcal.com/api_hours_today.php?iid=4097&lid=0&format=json&systemTime=0"

type tamuccLibHoursResponse struct {
	Raw    string `json:"raw"`
	Status string `json:"status"` // "open" or "closed"
	Open   string `json:"open"`
	Close  string `json:"close"`
	Speech string `json:"speech"`
}

type libCalHours struct {
	Locations []struct {
		Times struct {
			Status string `json:"status"`
			Hours  []struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"hours"`
		} `json:"times"`
		Rendered string `json:"rendered"`
	} `json:"locations"`
}

var libCalClient = &http.Client{Timeout: 10 * time.Second}

func (h *Handlers) TamuccLibHoursHandler(w http.ResponseWriter, r *http.Request) {
	resp, err := libCalClient.Get(tamuccLibHoursURL)
	if err != nil {
		log.Printf("tamucc-lib-hours fetch error: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to get library hours"})
		return
	}
	defer resp.Body.Close()

	var data libCalHours
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || len(data.Locations) == 0 {
		log.Printf("tamucc-lib-hours decode error: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to get library hours"})
		return
	}

	loc := data.Locations[0]
	out := tamuccLibHoursResponse{Raw: strings.ToUpper(loc.Rendered), Status: "open"}
	switch {
	case loc.Times.Status == "closed":
		out.Status = "closed"
		out.Speech = "The library is closed today."
	case len(loc.Times.Hours) > 0:
		out.Open = strings.ToUpper(loc.Times.Hours[0].From)
		out.Close = strings.ToUpper(loc.Times.Hours[len(loc.Times.Hours)-1].To)
		out.Speech = fmt.Sprintf("The library is open today from %s to %s.", out.Open, out.Close)
	default:
		// e.g. "24 Hours"
		out.Speech = fmt.Sprintf("The library's hours today are: %s.", out.Raw)
	}

	writeJSON(w, http.StatusOK, out)
}
