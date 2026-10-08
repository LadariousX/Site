package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	tamuccLibURL           = "https://www.tamucc.edu/library/"
	tamuccLibHoursSelector = `//*[@id="todays-hours-hours"]` // X-path selector for hours string
)

type tamuccLibHoursResponse struct {
	Raw    string `json:"raw"`
	Status string `json:"status"` // "open" or "closed"
	Open   string `json:"open"`
	Close  string `json:"close"`
	Speech string `json:"speech"`
}

func (h *Handlers) TamuccLibHoursHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := scrapeTamuccLibHours(r.Context())
	if err != nil {
		log.Printf("tamucc-lib-hours scrape error: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to get library hours"})
		return
	}

	out := tamuccLibHoursResponse{Raw: raw, Status: "open"}
	if open, close, ok := strings.Cut(raw, " - "); ok {
		out.Open = strings.TrimSpace(open)
		out.Close = strings.TrimSpace(close)
		out.Speech = fmt.Sprintf("The library is open today from %s to %s.", out.Open, out.Close)
	} else if strings.Contains(strings.ToLower(raw), "closed") {
		out.Status = "closed"
		out.Speech = "The library is closed today."
	} else {
		// e.g. "24 Hours"
		out.Speech = fmt.Sprintf("The library's hours today are: %s.", raw)
	}

	writeJSON(w, http.StatusOK, out)
}

// scrapeTamuccLibHours loads the library homepage in headless Chrome and reads
// today's hours once the page's script has filled them in.
func scrapeTamuccLibHours(ctx context.Context) (string, error) {
	// The timeout covers the whole browser, which only lives for this request.
	ctx, timeoutCancel := context.WithTimeout(ctx, time.Minute)
	defer timeoutCancel()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoSandbox, // needed when running as root in the container
		chromedp.DisableGPU,
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	err := chromedp.Do(ctx,
		chromedp.Navigate(tamuccLibURL),
		chromedp.WaitReady(tamuccLibHoursSelector),
	)
	if err != nil {
		return "", err
	}
	libHours, err := chromedp.Run(ctx, chromedp.Text(tamuccLibHoursSelector))
	if err != nil {
		return "", err
	}
	return strings.ToUpper(strings.TrimSpace(libHours)), nil
}
