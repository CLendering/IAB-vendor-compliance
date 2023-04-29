package main

import (
	"encoding/csv"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/SirDataFR/iabtcfv2"
	"github.com/tebeka/selenium"
	"github.com/tebeka/selenium/chrome"
)

func main() {
	// Set up Chrome driver service
	service, err := selenium.NewChromeDriverService("./chromedriver.exe", 4444)
	if err != nil {
		log.Fatal("Error starting Chrome driver service:", err)
	}
	defer service.Stop()

	// Set up Chrome capabilities
	chromeCaps := chrome.Capabilities{
		Args: []string{
			//"--headless",
			"--no-sandbox",
			"--disable-gpu",
			"--no-sandbox",
			"--disable-dev-shm-usage",
		},
	}
	caps := selenium.Capabilities{"browserName": "chrome"}
	caps.AddChrome(chromeCaps)
	// Open CSV file with domains implementing the TCFv2.0
	fd, err := os.Open("domains.csv")
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

	// Open file to write results to
	resultsFile, err := os.Create("latest_results.csv")
	if err != nil {
		log.Fatal("Error creating results.csv file:", err)
	}
	defer resultsFile.Close()

	// Create CSV writer
	resultswriter := csv.NewWriter(resultsFile)

	// Write header row to results CSV file
	header := []string{"Domain", "Condition", "cmpID", "FinaltcString", "generatedTCstring"}
	err = resultswriter.Write(header)
	if err != nil {
		log.Fatal("Error writing header row to results.csv file:", err)
	}
	resultswriter.Flush()

	for _, domain := range domains {

		driver, err := selenium.NewRemote(caps, "")
		if err != nil {
			continue
		}
		defer driver.Quit()

		timeout := 20 * time.Second
		if err := driver.SetImplicitWaitTimeout(timeout); err != nil {
			log.Fatal(err)
		}

		err = driver.SetPageLoadTimeout(5 * time.Second)
		if err != nil {
			driver.Quit()
			continue
		}

		// Navigate to the website
		err = driver.Get("https://" + domain[0])
		if err != nil {
			driver.Quit()
			continue

		}

		// Wait for the page to load
		time.Sleep(1 * time.Second)

		// Generate a valid TC string for that CMP and save it in a cookie and local storage on that domain

		cmpIDJS := "let cmpId = 0; window.__tcfapi('ping', 2, (PingReturn,success) => {cmpId = PingReturn.cmpId}); return cmpId"
		cmpVerJS := "let cmpv = 0; window.__tcfapi('ping', 2, (PingReturn,success) => {cmpv = PingReturn.cmpId}); return cmpv"
		gvlVerJS := "let gvl = 0; window.__tcfapi('ping', 2, (PingReturn,success) => {gvl = PingReturn.gvlVersion}); return gvl"
		displayStatusJS := "let ds = 0; window.__tcfapi('ping', 2, (PingReturn,success) => {ds = PingReturn.displayStatus}); return ds"
		tcStringJS := "let tc = \"\"; window.__tcfapi('getTCData',2,(tcData,success) => {tc = tcData.tcString}); return tc"
		cmpID, err := driver.ExecuteScript(cmpIDJS, nil)

		if err != nil {
			driver.Quit()
			continue
		}
		if cmpID != nil {
			cmpID = int(cmpID.(float64))
		} else {
			cmpID = 0
		}
		cmpVer, err := driver.ExecuteScript(cmpVerJS, nil)
		if err != nil {
			driver.Quit()
			continue
		}
		if cmpVer != nil {
			cmpVer = int(cmpVer.(float64))
		} else {
			cmpVer = 1
		}

		gvlVer, err := driver.ExecuteScript(gvlVerJS, nil)
		if err != nil {
			driver.Quit()
			continue
		}
		if gvlVer != nil {
			gvlVer = int(gvlVer.(float64))
		} else {
			gvlVer = 133
		}

		displayStatus, err := driver.ExecuteScript(displayStatusJS, nil)
		if err != nil {
			driver.Quit()
			continue
		}

		if displayStatus == nil {
			displayStatus = "visible"
		}

		dateStr := "Sun Mar 26 2023 01:00:00 GMT+0100"
		layout := "Mon Jan 02 2006 15:04:05 GMT-0700"

		encoded_time, err := time.Parse(layout, dateStr)

		tcData := &iabtcfv2.TCData{
			CoreString: &iabtcfv2.CoreString{
				Version:              2,
				Created:              encoded_time, //time.Now(),
				LastUpdated:          encoded_time, //time.Now(),
				CmpId:                cmpID.(int),
				CmpVersion:           cmpVer.(int),
				ConsentScreen:        1,
				ConsentLanguage:      "EN",
				VendorListVersion:    gvlVer.(int),
				TcfPolicyVersion:     2,
				IsServiceSpecific:    true,
				UseNonStandardStacks: false,
				PurposeOneTreatment:  false,
				PurposesConsent: map[int]bool{
					1: true,
					2: true,
					3: true,
					5: true,
				},
			},
		}

		// Generate a valid TC string for that CMP and save it in a cookie and local storage on that domain

		tcString := tcData.ToTCString()

		cookieJS := "document.cookie = 'euconsent-v2=" + tcString + "';document.cookie = 'eupubconsent-v2=" + tcString + "';"
		localStorageJS := "localStorage.setItem('euconsent-v2', '" + tcString + "');"
		localStorageJSv2 := "localStorage.setItem('eupubconsent-v2', '" + tcString + "');"
		_, err = driver.ExecuteScript(cookieJS, nil)
		if err != nil {
			driver.Quit()
			continue
		}
		_, err = driver.ExecuteScript(localStorageJS, nil)
		if err != nil {
			driver.Quit()
			continue
		}
		_, err = driver.ExecuteScript(localStorageJSv2, nil)
		if err != nil {
			driver.Quit()
			continue
		}

		// Reload the page
		err = driver.Get("https://" + domain[0])
		if err != nil {
			driver.Quit()
			continue
		}

		// Wait for the page to load
		time.Sleep(1 * time.Second)

		// Check the display status of the CMP
		displayStatusAfterReload, err := driver.ExecuteScript(displayStatusJS, nil)
		if err != nil {
			driver.Quit()
			continue
		}
		if displayStatusAfterReload == nil {
			displayStatusAfterReload = "visible"
		}

		tcStringAfterReload, err := driver.ExecuteScript(tcStringJS, nil)
		if err != nil {
			driver.Quit()
			continue
		}
		if tcStringAfterReload == nil {
			tcStringAfterReload = "dummy.string"
		}
		if displayStatus != displayStatusAfterReload && tcString == strings.Split(tcStringAfterReload.(string), ".")[0] {
			row := []string{domain[0], "1", strconv.Itoa(cmpID.(int)), tcStringAfterReload.(string), tcString}
			resultswriter.Write(row)
		} else if displayStatus == displayStatusAfterReload && tcString == strings.Split(tcStringAfterReload.(string), ".")[0] {
			row := []string{domain[0], "2", strconv.Itoa(cmpID.(int)), tcStringAfterReload.(string), tcString}
			resultswriter.Write(row)
		} else if displayStatus != displayStatusAfterReload && tcString != strings.Split(tcStringAfterReload.(string), ".")[0] {
			row := []string{domain[0], "3", strconv.Itoa(cmpID.(int)), tcStringAfterReload.(string), tcString}
			resultswriter.Write(row)
		} else {
			row := []string{domain[0], "0", strconv.Itoa(cmpID.(int)), tcStringAfterReload.(string), tcString}
			resultswriter.Write(row)
		}

		resultswriter.Flush()
		driver.Close()
		driver.Quit()
	}
	resultsFile.Close()
}
