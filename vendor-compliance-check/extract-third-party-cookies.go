package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SirDataFR/iabtcfv2"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/elazarl/goproxy"
)

const (
	proxyAddr       = "localhost:8080"
	cmpIDJS         = "function getcmpID() {let cmpId = 0; window.__tcfapi('ping', 2, (PingReturn,success) => {cmpId = PingReturn.cmpId}); return cmpId } getcmpID()"
	cmpVerJS        = "let cmpv = 0; window.__tcfapi('ping', 2, (PingReturn,success) => {cmpv = PingReturn.cmpVersion}); cmpv"
	gvlVerJS        = "let gvl = 0; window.__tcfapi('ping', 2, (PingReturn,success) => {gvl = PingReturn.gvlVersion}); gvl"
	displayStatusJS = "let ds = 0; window.__tcfapi('ping', 2, (PingReturn,success) => {ds = PingReturn.displayStatus}); ds"
)

type tcpKeepAliveListener struct {
	*net.TCPListener
}

func saveProgress(index int) {
	f, err := os.Create("progress.txt")
	if err != nil {
		log.Printf("Error creating progress file: %v", err)
		return
	}
	defer f.Close()

	_, err = f.WriteString(strconv.Itoa(index))
	if err != nil {
		log.Printf("Error writing progress file: %v", err)
	}
}

func loadProgress() int {
	data, err := ioutil.ReadFile("progress.txt")
	if err != nil {
		log.Printf("Error reading progress file: %v", err)
		return 0
	}

	index, err := strconv.Atoi(string(data))
	if err != nil {
		log.Printf("Error converting progress to int: %v", err)
		return 0
	}

	return index
}

func isCookieExpired(cookie *http.Cookie) bool {
	if cookie == nil {
		return true
	}
	if cookie.Expires.IsZero() {
		return true
	}
	return cookie.Expires.Before(time.Now())
}

func (ln tcpKeepAliveListener) Accept() (net.Conn, error) {
	conn, err := ln.TCPListener.Accept()
	if err != nil {
		return nil, err
	}

	conn.(*net.TCPConn).SetKeepAlive(true)
	conn.(*net.TCPConn).SetKeepAlivePeriod(3 * time.Minute)
	return conn, nil
}

func waitForTcfApi(timeout time.Duration) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		var isApiReady bool
		var cmpId float64 = 0
		startTime := time.Now()

		for !isApiReady || cmpId == 0 {
			chromedp.Evaluate(`typeof window.__tcfapi === 'function'`, &isApiReady).Do(ctx)
			chromedp.EvaluateAsDevTools(cmpIDJS, &cmpId).Do(ctx)
			time.Sleep(1 * time.Second)

			// Break the loop if it has been running for more than the specified timeout
			if time.Since(startTime) > timeout {
				break
			}
		}

		return nil
	})
}

func setConsent(tcString *string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		var cmpID float64 = -1
		if err := chromedp.EvaluateAsDevTools(cmpIDJS, &cmpID).Do(ctx); err != nil {
			return err
		}

		var intCmpID int
		if cmpID != -1 {
			intCmpID = int(cmpID)
		} else {
			intCmpID = 0
		}

		var cmpVer float64 = -1
		if err := chromedp.EvaluateAsDevTools(cmpVerJS, &cmpVer).Do(ctx); err != nil {
			return err
		}

		var intCmpVer int
		if cmpVer != -1 {
			intCmpVer = int(cmpVer)
		} else {
			intCmpVer = 1 // set a default value
		}

		var gvlVer float64 = -1
		chromedp.Evaluate(gvlVerJS, &gvlVer)

		var intGvlVer int
		if gvlVer != -1 {
			intGvlVer = int(gvlVer)
		} else {
			intGvlVer = 189 // set a default value
		}

		dateStr := "Sun Apr 09 2023 01:00:00 GMT+0100"
		layout := "Mon Jan 02 2006 15:04:05 GMT-0700"

		encoded_time, err := time.Parse(layout, dateStr)
		if err != nil {
			log.Fatalf("Failed to parse time:. %v", err)
		}

		tcData := &iabtcfv2.TCData{
			CoreString: &iabtcfv2.CoreString{
				Version:                2,
				Created:                encoded_time,
				LastUpdated:            encoded_time,
				CmpId:                  intCmpID,
				CmpVersion:             intCmpVer,
				ConsentScreen:          2,
				ConsentLanguage:        "EN",
				VendorListVersion:      intGvlVer,
				TcfPolicyVersion:       2,
				IsServiceSpecific:      true,
				SpecialFeatureOptIns:   map[int]bool{},
				UseNonStandardStacks:   false,
				PurposesConsent:        map[int]bool{},
				PurposesLITransparency: map[int]bool{},
				PurposeOneTreatment:    true,
				PublisherCC:            "NL",
				IsRangeEncoding:        true,
				VendorsConsent:         map[int]bool{},
				MaxVendorId:            1200,
				NumEntries:             1,
				RangeEntries: []*iabtcfv2.RangeEntry{
					{
						StartVendorID: 1,
						EndVendorID:   1200,
					},
				},
				VendorsLITransparency: map[int]bool{},
			},
			PublisherTC: &iabtcfv2.PublisherTC{
				SegmentType:               3,
				PubPurposesConsent:        map[int]bool{},
				PubPurposesLITransparency: map[int]bool{},
			},
		}

		// Generate a valid TC string for that CMP and save it in a cookie and local storage on that domain

		consentString := tcData.ToTCString()

		cookieJS := "document.cookie = 'euconsent-v2=" + consentString + "';document.cookie = 'eupubconsent-v2=" + consentString + "';"
		localStorageJS := "localStorage.setItem('euconsent-v2', '" + consentString + "');"
		localStorageJSv2 := "localStorage.setItem('eupubconsent-v2', '" + consentString + "');"

		if err := chromedp.EvaluateAsDevTools(cookieJS, nil).Do(ctx); err != nil {
			log.Fatal(err)
		}
		if err := chromedp.EvaluateAsDevTools(localStorageJS, nil).Do(ctx); err != nil {
			log.Fatal(err)
		}
		if err := chromedp.EvaluateAsDevTools(localStorageJSv2, nil).Do(ctx); err != nil {
			log.Fatal(err)
		}

		*tcString = consentString
		return nil
	})
}

func getTCstring(apiResponse *string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		const tcStringJS = `
			new Promise((resolve) => {
				if (typeof window.__tcfapi === 'function') {
					callGetTCData();
				} else {
					window.addEventListener('cmpLoaded', callGetTCData);
				}

				function callGetTCData() {
					window.__tcfapi('getTCData', 2, (tcData, success) => {
						if (success) {
							resolve({tcString: tcData.tcString});
						} else {
							resolve(null);
						}
					});
				}
			})
		`

		var jsonResponse struct {
			TCString string `json:"tcString"`
		}

		if err := chromedp.Evaluate(tcStringJS, &jsonResponse, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}).Do(ctx); err != nil {
			log.Printf("Error querying the TC string: %v", err)
		}

		*apiResponse = jsonResponse.TCString
		return nil
	})
}

func getTcEventStatus(eventStatus *string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		const tcEventStatusJS = `
			new Promise((resolve) => {
				if (typeof window.__tcfapi === 'function') {
					callGetTCData();
				} else {
					window.addEventListener('cmpLoaded', callGetTCData);
				}

				function callGetTCData() {
					window.__tcfapi('getTCData', 2, (tcData, success) => {
						if (success) {
							resolve({tcEventStatus: tcData.eventStatus});
						} else {
							resolve(null);
						}
					});
				}
			})
		`

		var jsonResponse struct {
			TcEventStatus string `json:"tcEventStatus"`
		}

		if err := chromedp.Evaluate(tcEventStatusJS, &jsonResponse, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}).Do(ctx); err != nil {
			log.Printf("Error querying the Event Status: %v", err)
		}

		*eventStatus = jsonResponse.TcEventStatus
		return nil
	})
}

func run(targetURL string, ctx context.Context) ([]*http.Cookie, string, string, string, string) {

	var cookies []*http.Cookie
	// Set up the HTTP proxy
	proxy := goproxy.NewProxyHttpServer()
	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)

	// Custom logging function to handle proxy warnings
	proxy.Verbose = true
	customLogger := log.New(os.Stderr, "ProxyLog: ", log.LstdFlags)
	proxy.Logger = customLogger

	var mu sync.Mutex
	var wg sync.WaitGroup

	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if strings.Contains(req.URL.Host, targetURL) {
			// add cookies to the request
			for _, cookie := range cookies {
				req.AddCookie(cookie)
			}
		}

		return req, nil
	})

	proxy.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		wg.Add(1)
		defer wg.Done()

		if resp != nil && resp.Request != nil {
			if !strings.Contains(resp.Request.URL.Host, targetURL) {
				for _, newCookie := range resp.Cookies() {
					var existingCookie *http.Cookie
					for _, c := range cookies {
						if c.Name == newCookie.Name && c.Domain == newCookie.Domain {
							existingCookie = c
							break
						}
					}
					if existingCookie == nil {
						mu.Lock()
						cookies = append(cookies, newCookie)
						mu.Unlock()
					} else {
						existingCookie.Value = newCookie.Value
					}
				}
			}
		}

		return resp
	})

	// Start the proxy server using a custom listener
	listener, err := net.Listen("tcp", proxyAddr)
	if err != nil {
		log.Printf("Error creating listener: %v", err)
	}
	defer listener.Close()

	server := &http.Server{
		Addr:         proxyAddr,
		Handler:      proxy,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	defer server.Close()

	go func() {
		if err := server.Serve(tcpKeepAliveListener{listener.(*net.TCPListener)}); err != nil && err != http.ErrServerClosed {
			log.Printf("Error starting server: %v", err)
		}
	}()
	defer func() {
		wg.Wait()
		ctxShutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		server.Shutdown(ctxShutdown)
	}()

	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *network.EventResponseReceived:
			log.Printf("Received response for URL: %s", ev.Response.URL)
		}
	})

	// Create a new context with a timeout of 60 seconds
	timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var tcString string
	var apiTcString string

	var eventStatusBeforeRL string
	var eventStatusAfterRL string

	if err := chromedp.Run(timeoutCtx,
		network.Enable(),
		chromedp.Navigate(targetURL),
		waitForTcfApi(10*time.Second),
		getTcEventStatus(&eventStatusBeforeRL),
		setConsent(&tcString),
		chromedp.Reload(),
		waitForTcfApi(10*time.Second),
		getTCstring(&apiTcString),
		getTcEventStatus(&eventStatusAfterRL),
		chromedp.Navigate("about:blank"),
	); err != nil {
		log.Printf("Encountered an error running chromedp: %v", err)
	}
	cancel()

	return cookies, tcString, apiTcString, eventStatusBeforeRL, eventStatusAfterRL
}

func main() {
	// read domains
	fd, err := os.Open("trackers_dataset.csv")
	if err != nil {
		log.Fatal("Error opening domains.csv file:", err)
	}
	defer fd.Close()

	// Read CSV file
	fileReader := csv.NewReader(fd)
	domains, err := fileReader.ReadAll()
	if err != nil {
		log.Fatal("Error reading domains.csv file:", err)
	}

	// Write cookies to a CSV file
	file, err := os.OpenFile("deny_all_purposes_accept_all_vendors.csv", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	fileInfo, err := file.Stat()
	if err != nil {
		log.Fatal(err)
	}

	if fileInfo.Size() == 0 {
		writer.Write([]string{"Website", "Domain", "Name", "Value", "Path", "Expires", "IsExpired", "Generated Consent String", "API Consent String", "StringsEqual", "EventStatus b4", "EventStatus after", "Status Updated"})
		writer.Flush()
	}

	// Set up Chrome with the HTTP proxy
	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ProxyServer(proxyAddr),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.DisableGPU,
		chromedp.NoSandbox,
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("headless", false),
	)...)
	defer cancel()

	lastProcessedIndex := loadProgress()

	for index, domain := range domains {

		// Skip domains that have already been processed
		if index < lastProcessedIndex {
			continue
		}

		ctx, cancelCtx := chromedp.NewContext(allocCtx, chromedp.WithLogf(log.Printf))
		defer cancel()

		targetURL := "https://" + domain[0]
		var cookies, tcString, apiTcString, eventStatusBeforeRL, eventStatusAfterRL = run(targetURL, ctx)
		for _, c := range cookies {
			if !isCookieExpired(c) {
				writer.Write([]string{domain[0], c.Domain, c.Name, c.Value, c.Path, c.Expires.Format(time.RFC1123), fmt.Sprint(isCookieExpired(c)), tcString, apiTcString, fmt.Sprint(strings.Split(tcString, ".")[0] == strings.Split(apiTcString, ".")[0]), eventStatusBeforeRL, eventStatusAfterRL, fmt.Sprint(eventStatusBeforeRL != eventStatusAfterRL)})
				writer.Flush()
			}
		}
		fmt.Printf("Done with domain: %v", domain[0])
		saveProgress(index + 1)
		cancelCtx()
	}

	saveProgress(0)

}
