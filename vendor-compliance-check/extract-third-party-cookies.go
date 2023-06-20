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
	// Set the address and port for the proxy server
	proxyAddr = "localhost:8080"

	// JavaScript to extract CMP related details
	cmpIDJS         = "function getcmpID() {let cmpId = 0; window.__tcfapi('ping', 2, (PingReturn,success) => {cmpId = PingReturn.cmpId}); return cmpId } getcmpID()"
	cmpVerJS        = "let cmpv = 0; window.__tcfapi('ping', 2, (PingReturn,success) => {cmpv = PingReturn.cmpVersion}); cmpv"
	gvlVerJS        = "let gvl = 0; window.__tcfapi('ping', 2, (PingReturn,success) => {gvl = PingReturn.gvlVersion}); gvl"
	displayStatusJS = "let ds = 0; window.__tcfapi('ping', 2, (PingReturn,success) => {ds = PingReturn.displayStatus}); ds"
	tcStringJS      = `
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
	tcEventStatusJS = `
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

	// Set TimeOut values
	ReadTimeout        = 30 * time.Second // ReadTimeout specifies the maximum duration for reading the entire HTTP request, including the request headers and body, from the client.
	WriteTimeout       = 30 * time.Second // WriteTimeout specifies the maximum duration allowed for writing the HTTP response back to the client.
	IdleTimeout        = 60 * time.Second // IdleTimeout specifies the maximum duration of idle time allowed after the last HTTP request has been served.
	ShutdownTimeout    = 5 * time.Second  // ShutdownTimeout specifies the maximum duration of time allowed to gracefully shutdown the HTTP server.
	RunTimeout         = 60 * time.Second // RunTimeout specifies the the maximum duration of time allowed to run chromedp for a single domain.
	TCFTimeOut         = 10 * time.Second // TCFTimeOut  specifies the the maximum duration of time allowed to wait for the TCF API to become available.
	TCFWaitInterval    = 1 * time.Second  // TCFWaitIntervalpecifies the the maximum duration of time between queries to the TCF API.
	TCPKeepAlivePeriod = 30 * time.Second // TCPKeepAlivePeriod specifies the duration between TCP keep-alive probes sent by a server to check if a connection is alive.

	// Specify input/output files
	DomainsFile = "cat_1_rerun.csv"
	OutputFile  = "output.csv"
)

// type for TCP KeepAlive Listener
type tcpKeepAliveListener struct {
	*net.TCPListener
}

// saveProgress stores the progress index into a file
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

// loadProgress retrieves the progress index from a file
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

// isCookieExpired checks if a cookie has expired
func isCookieExpired(cookie *http.Cookie) bool {
	if cookie == nil {
		return true
	}
	if cookie.Expires.IsZero() {
		return true
	}
	if cookie.MaxAge < 0 {
		return true
	}
	return cookie.Expires.Before(time.Now())
}

// Accept establishes a new connection with keep-alive enabled
func (ln tcpKeepAliveListener) Accept() (net.Conn, error) {
	conn, err := ln.TCPListener.Accept()
	if err != nil {
		return nil, err
	}

	conn.(*net.TCPConn).SetKeepAlive(true)
	conn.(*net.TCPConn).SetKeepAlivePeriod(TCPKeepAlivePeriod)
	return conn, nil
}

// waitForTcfApi waits for the TCF API to load on the webpage, or until the specified timeout has passed
func waitForTcfApi(timeout time.Duration) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		var isApiReady bool
		var cmpId float64 = 0
		startTime := time.Now()

		for !isApiReady || cmpId == 0 {
			chromedp.Evaluate(`typeof window.__tcfapi === 'function'`, &isApiReady).Do(ctx)
			chromedp.EvaluateAsDevTools(cmpIDJS, &cmpId).Do(ctx)
			time.Sleep(TCFWaitInterval)

			// Break the loop if it has been running for more than the specified timeout
			if time.Since(startTime) > timeout {
				break
			}
		}

		return nil
	})
}

// setConsent function accepts a pointer to a string representing the consent,
// then generates a valid TC (Transparency & Consent Framework) string,
// and stores it in a cookie and local storage on the domain.
// setConsent function sets up user's consent data.
func setConsent(tcString *string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		intCmpID, err := evaluateJSAndGetInteger(ctx, cmpIDJS)
		if err != nil {
			return err
		}

		intCmpVer, err := evaluateJSAndGetInteger(ctx, cmpVerJS, 1) // set a default value
		if err != nil {
			return err
		}

		intGvlVer, err := evaluateJSAndGetInteger(ctx, gvlVerJS, 189) // set a default value
		if err != nil {
			return err
		}

		tcData := buildTCData(intCmpID, intCmpVer, intGvlVer)

		consentString := tcData.ToTCString()

		*tcString = consentString
		return storeConsentInBrowser(ctx, consentString)
	})
}

// evaluateJSAndGetInteger evaluates a JavaScript snippet and returns the resulting value as an integer.
func evaluateJSAndGetInteger(ctx context.Context, js string, defaultValue ...int) (int, error) {
	var value float64 = -1
	if err := chromedp.EvaluateAsDevTools(js, &value).Do(ctx); err != nil {
		return 0, err
	}

	if value != -1 {
		return int(value), nil
	}

	if len(defaultValue) > 0 {
		return defaultValue[0], nil
	}
	return 0, nil
}

// buildTCData builds and returns a pointer to a TCData object.
func buildTCData(intCmpID, intCmpVer, intGvlVer int) *iabtcfv2.TCData {
	return &iabtcfv2.TCData{
		CoreString: &iabtcfv2.CoreString{
			Version:              2,
			Created:              time.Now(),
			LastUpdated:          time.Now(),
			CmpId:                intCmpID,
			CmpVersion:           intCmpVer,
			ConsentScreen:        2,
			ConsentLanguage:      "EN",
			VendorListVersion:    intGvlVer,
			TcfPolicyVersion:     2,
			IsServiceSpecific:    true,
			SpecialFeatureOptIns: map[int]bool{},
			UseNonStandardStacks: false,
			PurposesConsent: map[int]bool{
				1:  true,
				2:  true,
				3:  true,
				4:  true,
				5:  true,
				6:  true,
				7:  true,
				8:  true,
				9:  true,
				10: true,
			},
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
}

// storeConsentInBrowser stores the consent string in a cookie and local storage.
func storeConsentInBrowser(ctx context.Context, consentString string) error {
	// save the TC string in a cookie and local storage on the domain
	jsActions := []string{
		"document.cookie = 'euconsent-v2=" + consentString + "';document.cookie = 'eupubconsent-v2=" + consentString + "';",
		"localStorage.setItem('euconsent-v2', '" + consentString + "');",
		"localStorage.setItem('eupubconsent-v2', '" + consentString + "');",
	}

	for _, js := range jsActions {
		if err := chromedp.EvaluateAsDevTools(js, nil).Do(ctx); err != nil {
			return err
		}
	}

	return nil
}

// getTCstring is a function that returns a chromedp Action which fetches the TC string from a website.
func getTCstring(apiResponse *string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {

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

// getTcEventStatus is a function that returns a chromedp Action which fetches the CMP's eventStatus from a website.
func getTcEventStatus(eventStatus *string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {

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

// Initialize the HTTP proxy server
func initializeProxyServer() *goproxy.ProxyHttpServer {
	proxy := goproxy.NewProxyHttpServer()
	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)
	proxy.Verbose = true
	customLogger := log.New(os.Stderr, "ProxyLog: ", log.LstdFlags)
	proxy.Logger = customLogger

	return proxy
}

// Update the cookie list
func updateCookieList(cookies *[]*http.Cookie, newCookie *http.Cookie, mu *sync.Mutex) {
	mu.Lock()
	defer mu.Unlock()

	var found bool
	var index int
	for i, c := range *cookies {
		if c.Name == newCookie.Name && c.Domain == newCookie.Domain {
			index = i
			found = true
			break
		}
	}

	if !found {
		*cookies = append(*cookies, newCookie)
	} else {
		(*cookies)[index] = newCookie
	}
}

// Run the Chrome Developer Protocol
func runChromedp(ctx context.Context, targetURL string) (string, string, string, string) {
	timeoutCtx, cancel := context.WithTimeout(ctx, RunTimeout)
	defer cancel()

	var tcString string
	var apiTcString string

	var eventStatusBeforeRL string
	var eventStatusAfterRL string

	if err := chromedp.Run(timeoutCtx,
		network.Enable(),
		chromedp.Navigate(targetURL),
		waitForTcfApi(TCFTimeOut),
		getTcEventStatus(&eventStatusBeforeRL),
		setConsent(&tcString),
		chromedp.Reload(),
		waitForTcfApi(TCFTimeOut),
		getTCstring(&apiTcString),
		getTcEventStatus(&eventStatusAfterRL),
		chromedp.Navigate("about:blank"),
	); err != nil {
		log.Printf("Encountered an error running chromedp: %v", err)
	}

	return tcString, apiTcString, eventStatusBeforeRL, eventStatusAfterRL
}

// run is a function that initiates a proxy server, captures cookies,
// generates and sets user consent, and fetches the TC string from a target website.
//
// It accepts a target URL and a context, launches a headless browser
// and navigates to the target URL.
//
// The function returns all cookies captured, the generated TCF string,
// the fetched TCF string, and the status of the TCF API before and after reload.
func run(targetURL string, ctx context.Context) ([]*http.Cookie, string, string, string, string) {

	var cookies []*http.Cookie
	var mu sync.Mutex
	var wg sync.WaitGroup

	proxy := initializeProxyServer()

	// Handle requests coming through the proxy server
	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if strings.Contains(req.URL.Host, targetURL) {
			// add cookies to the request
			for _, cookie := range cookies {
				req.AddCookie(cookie)
			}
		}

		return req, nil
	})

	// Handle responses coming from the proxy server
	proxy.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		wg.Add(1)
		defer wg.Done()

		if resp != nil && resp.Request != nil {
			if !strings.Contains(resp.Request.URL.Host, targetURL) {
				for _, newCookie := range resp.Cookies() {
					updateCookieList(&cookies, newCookie, &mu)
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
		ReadTimeout:  ReadTimeout,
		WriteTimeout: WriteTimeout,
		IdleTimeout:  IdleTimeout,
	}
	defer server.Close()

	// Start the proxy server in a separate goroutine (The Serve method of the proxy server is a blocking operation)
	go func() {
		if err := server.Serve(tcpKeepAliveListener{listener.(*net.TCPListener)}); err != nil && err != http.ErrServerClosed {
			log.Printf("Error starting server: %v", err)
		}
	}()
	// Wait for all goroutines to finish and gracefully shut down the server
	defer func() {
		wg.Wait()
		// Create a context with a timeout for server shutdown
		ctxShutdown, cancelShutdown := context.WithTimeout(context.Background(), ShutdownTimeout)
		defer cancelShutdown()
		// Shutdown the server
		server.Shutdown(ctxShutdown)
	}()

	// Listen for network events using chromedp
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *network.EventResponseReceived:
			log.Printf("Received response for URL: %s", ev.Response.URL)
		}
	})

	// Run chromedp commands and retrieve values
	tcString, apiTcString, eventStatusBeforeRL, eventStatusAfterRL := runChromedp(ctx, targetURL)

	return cookies, tcString, apiTcString, eventStatusBeforeRL, eventStatusAfterRL
}

// Read domains from a CSV file
func readDomainsFromFile(filename string) ([]string, error) {
	fd, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	fileReader := csv.NewReader(fd)
	domains, err := fileReader.ReadAll()
	if err != nil {
		return nil, err
	}

	var result []string
	for _, domain := range domains {
		if len(domain) > 0 {
			result = append(result, domain[0])
		}
	}

	return result, nil
}

// Open the output CSV file
func openCSVFile(filename string) (*os.File, error) {
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return file, nil
}

// Check if the file is empty
func isEmptyFile(file *os.File) bool {
	fileInfo, err := file.Stat()
	if err != nil {
		return true
	}
	return fileInfo.Size() == 0
}

// Create the Chrome context
func createChromeContext() (context.Context, context.CancelFunc) {
	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ProxyServer(proxyAddr),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("headless", false),
	)...)
	return allocCtx, cancel
}

// Create a domain-specific Chrome context
func createDomainContext(allocCtx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(log.Printf))
	return ctx, cancel
}

func main() {
	// Read domains from CSV file
	domains, err := readDomainsFromFile(DomainsFile)
	if err != nil {
		log.Fatal("Error reading domains:", err)
	}

	// Open the output CSV file
	file, err := openCSVFile(OutputFile)
	if err != nil {
		log.Fatal("Error opening output file:", err)
	}
	defer file.Close()

	// Initialize CSV writer
	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header if the file is empty
	if isEmptyFile(file) {
		writer.Write([]string{"Website", "Domain", "Name", "Value", "Path", "Expires", "IsExpired", "Generated Consent String", "API Consent String", "StringsEqual", "EventStatus b4", "EventStatus after", "Status Updated"})
		writer.Flush()
	}

	// Set up Chrome with the HTTP proxy
	allocCtx, cancel := createChromeContext()
	defer cancel()

	// Load last processed index
	lastProcessedIndex := loadProgress()

	// Process domains
	for index, domain := range domains {
		// Skip domains that have already been processed
		if index < lastProcessedIndex {
			continue
		}

		// Create a new Chrome context for each domain
		ctx, cancelCtx := createDomainContext(allocCtx)

		targetURL := "https://" + domain
		cookies, tcString, apiTcString, eventStatusBeforeRL, eventStatusAfterRL := run(targetURL, ctx)

		// Write non-expired cookies to a CSV file
		for _, c := range cookies {
			if !isCookieExpired(c) {
				writer.Write([]string{domain, c.Domain, c.Name, c.Value, c.Path, c.Expires.Format(time.RFC1123), fmt.Sprint(isCookieExpired(c)), tcString, apiTcString, fmt.Sprint(strings.Split(tcString, ".")[0] == strings.Split(apiTcString, ".")[0]), eventStatusBeforeRL, eventStatusAfterRL, fmt.Sprint(eventStatusBeforeRL != eventStatusAfterRL)})
				writer.Flush()
			}
		}

		fmt.Printf("Done with domain: %v\n", domain)
		saveProgress(index + 1)
		cancelCtx()
	}

	// Reset progress
	saveProgress(0)
}
