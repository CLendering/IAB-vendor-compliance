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

// Constants related to the configuration of the chrome driver and the JS scripts to be executed.
const (
	ChromeDriverPath = "driver_path"
	Port             = 8080
	TCFDomainsFile   = "domains.csv"
	ResultsFile      = "output.csv"
	PageLoadTimeout  = 30 * time.Second

	cmpIDJS         = "let cmpId = 0; window.__tcfapi('ping', 2, (PingReturn,success) => {cmpId = PingReturn.cmpId}); return cmpId"
	cmpVerJS        = "let cmpv = 0; window.__tcfapi('ping', 2, (PingReturn,success) => {cmpv = PingReturn.cmpId}); return cmpv"
	gvlVerJS        = "let gvl = 0; window.__tcfapi('ping', 2, (PingReturn,success) => {gvl = PingReturn.gvlVersion}); return gvl"
	displayStatusJS = "let ds = \"\"; window.__tcfapi('ping', 2, (PingReturn,success) => {ds = PingReturn.displayStatus}); return ds"
	tcStringJS      = "let tc = \"\"; window.__tcfapi('getTCData',2,(tcData,success) => {tc = tcData.tcString}); return tc"
)

// setChromeCapabilities sets up the chrome capabilities for selenium.
func setChromeCapabilities() selenium.Capabilities {
	chromeCaps := chrome.Capabilities{
		Args: []string{
			"--disable-gpu",
			"--ignore-certificate-errors",
		},
	}
	caps := selenium.Capabilities{"browserName": "chrome"}
	caps.AddChrome(chromeCaps)

	return caps
}

// readCSV reads and returns the content of a CSV file.
func readCSV(filename string) ([][]string, error) {
	fd, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	fileReader := csv.NewReader(fd)
	return fileReader.ReadAll()
}

// createCSVWriter creates a new CSV file and returns its file descriptor and a CSV writer.
func createCSVWriter(filename string) (*os.File, *csv.Writer, error) {
	resultsFile, err := os.Create(filename)
	if err != nil {
		return nil, nil, err
	}

	resultswriter := csv.NewWriter(resultsFile)

	header := []string{"Domain", "Condition", "CmpID", "FinalTCString", "GeneratedTCString"}
	err = resultswriter.Write(header)
	if err != nil {
		return nil, nil, err
	}
	resultswriter.Flush()

	return resultsFile, resultswriter, nil
}

// setPageLoadTimeout sets the page load timeout for the selenium web driver.
func setPageLoadTimeout(driver selenium.WebDriver, timeout time.Duration) error {
	if err := driver.SetImplicitWaitTimeout(timeout); err != nil {
		log.Println(err)
		driver.Quit()
		return err
	}

	if err := driver.SetPageLoadTimeout(timeout); err != nil {
		log.Println(err)
		driver.Quit()
		return err
	}
	return nil
}

// navigateWebsite navigates the selenium web driver to the given domain.
func navigateWebsite(driver selenium.WebDriver, domain string) error {
	err := driver.Get("https://" + domain)
	if err != nil {
		log.Println(err)
		driver.Quit()
	}
	return err
}

// executeScriptAndQuitOnError executes a JavaScript script and terminates the current session if there is an error.
func executeScriptAndQuitOnError(driver selenium.WebDriver, js string) (interface{}, error) {
	res, err := driver.ExecuteScript(js, nil)
	if err != nil {
		driver.Quit()
	}
	return res, err
}

// parseIntegerResult parses the given value to an integer.
func parseIntegerResult(value interface{}, defaultValue int) int {
	if value != nil {
		return int(value.(float64))
	}
	return defaultValue
}

// parseStringResult parses the given value to a string.
func parseStringResult(value interface{}, defaultValue string) string {
	if value != nil {
		return value.(string)
	}
	return defaultValue
}

// setCookiesAndLocalStorage sets the 'euconsent-v2' and 'eupubconsent-v2' cookies and local storage items to the given TC string.
func setCookiesAndLocalStorage(driver selenium.WebDriver, tcString string) error {
	cookieJS := "document.cookie = 'euconsent-v2=" + tcString + "';document.cookie = 'eupubconsent-v2=" + tcString + "';"
	localStorageJS := "localStorage.setItem('euconsent-v2', '" + tcString + "');localStorage.setItem('eupubconsent-v2', '" + tcString + "');"

	_, err := executeScriptAndQuitOnError(driver, cookieJS)
	if err != nil {
		return err
	}
	_, err = executeScriptAndQuitOnError(driver, localStorageJS)
	if err != nil {
		return err
	}
	return nil
}

// generateAndSetTCData generates a TCData object and sets the 'euconsent-v2' and 'eupubconsent-v2' cookies and local storage items to its string representation.
func generateAndSetTCData(driver selenium.WebDriver, cmpID int, cmpVer int, gvlVer int) (string, error) {

	// Get the current date and time
	currentTime := time.Now()

	tcData := &iabtcfv2.TCData{
		CoreString: &iabtcfv2.CoreString{
			Version:           2,
			Created:           currentTime,
			LastUpdated:       currentTime,
			CmpId:             cmpID,
			CmpVersion:        cmpVer,
			ConsentScreen:     1,
			ConsentLanguage:   "EN",
			VendorListVersion: gvlVer,
			TcfPolicyVersion:  2,
			IsServiceSpecific: true,
			PurposesConsent:   map[int]bool{},
		},
	}

	tcString := tcData.ToTCString()
	err := setCookiesAndLocalStorage(driver, tcString)
	if err != nil {
		return "", err
	}
	return tcString, nil
}

// writeRow writes a row of data to the CSV file.
func writeRow(driver selenium.WebDriver, writer *csv.Writer, domain string, tcString string, cmpID int, statusAfter string, tcStringAfterReload string) {
	// Prepare row data

	var row []string
	if statusAfter != "visible" && tcString == strings.Split(tcStringAfterReload, ".")[0] {
		row = []string{domain, "1", strconv.Itoa(cmpID), tcStringAfterReload, tcString}
	} else if statusAfter == "visible" && tcString == strings.Split(tcStringAfterReload, ".")[0] {
		row = []string{domain, "2", strconv.Itoa(cmpID), tcStringAfterReload, tcString}
	} else if statusAfter != "visible" && tcString != strings.Split(tcStringAfterReload, ".")[0] {
		row = []string{domain, "3", strconv.Itoa(cmpID), tcStringAfterReload, tcString}
	} else {
		row = []string{domain, "0", strconv.Itoa(cmpID), tcStringAfterReload, tcString}
	}

	err := writer.Write(row)
	if err != nil {
		log.Fatalf("Error while writing row data: %v", err)
		driver.Quit()
	}
	writer.Flush()
}

// navigateAndCheckStatus navigates to a website, checks the CMP's status and writes it to the CSV file.
func navigateAndCheckStatus(driver selenium.WebDriver, domain string, tcString string, cmpID int, tcStringJS string, resultswriter *csv.Writer) error {
	// Reload the page
	err := navigateWebsite(driver, domain)
	if err != nil {
		return err
	}

	displayStatusAfterReload, err := executeScriptAndQuitOnError(driver, displayStatusJS)
	if err != nil {
		return err
	}
	statusAfter := parseStringResult(displayStatusAfterReload, "noStatus")

	tcStringAfterReload, err := executeScriptAndQuitOnError(driver, tcStringJS)
	if err != nil {
		return err
	}
	tcStringAfter := parseStringResult(tcStringAfterReload, "dummy.string")

	writeRow(driver, resultswriter, domain, tcString, cmpID, statusAfter, tcStringAfter)

	return nil
}

// main sets up the ChromeDriver service, reads a CSV file of domains, creates a new CSV writer for the results,
// navigates to each domain, retrieves the CMP ID, version, and GVL version, generates and sets TC data, navigates back to the domain and checks
// the CMP's status, and finally writes the results to the CSV file.
func main() {
	// Set up Chrome driver service
	service, err := selenium.NewChromeDriverService(ChromeDriverPath, Port)
	if err != nil {
		log.Fatal("Error starting Chrome driver service:", err)
	}
	defer service.Stop()

	// Set up Chrome capabilities
	caps := setChromeCapabilities()

	// Read CSV file
	domains, err := readCSV(TCFDomainsFile)
	if err != nil {
		log.Fatalf("Error reading %s file: %v", TCFDomainsFile, err)
	}

	// Open file to write results to and Create CSV writer
	resultsFile, resultswriter, err := createCSVWriter(ResultsFile)
	if err != nil {
		log.Fatalf("Error creating %s file: %v", ResultsFile, err)
	}
	defer resultsFile.Close()

	for _, domain := range domains {

		driver, err := selenium.NewRemote(caps, "")
		if err != nil {
			log.Println(err)
			continue
		}
		defer driver.Quit()

		if err = setPageLoadTimeout(driver, PageLoadTimeout); err != nil {
			log.Println(err)
			continue
		}

		// Navigate to the website
		if err = navigateWebsite(driver, domain[0]); err != nil {
			log.Println(err)
			continue
		}

		cmpIDRes, err := executeScriptAndQuitOnError(driver, cmpIDJS)
		if err != nil {
			log.Println(err)
			continue
		}
		cmpID := parseIntegerResult(cmpIDRes, 0)

		cmpVerRes, err := executeScriptAndQuitOnError(driver, cmpVerJS)
		if err != nil {
			log.Println(err)
			continue
		}
		cmpVer := parseIntegerResult(cmpVerRes, 1)

		gvlVerRes, err := executeScriptAndQuitOnError(driver, gvlVerJS)
		if err != nil {
			log.Println(err)
			continue
		}
		gvlVer := parseIntegerResult(gvlVerRes, 133)

		// Generate a valid TC string for that CMP and save it in a cookie and local storage on that domain
		tcString, err := generateAndSetTCData(driver, cmpID, cmpVer, gvlVer)
		if err != nil {
			log.Println(err)
			continue
		}

		err = navigateAndCheckStatus(driver, domain[0], tcString, cmpID, tcStringJS, resultswriter)
		if err != nil {
			log.Println(err)
			continue
		}

		err = driver.Close()
		if err != nil {
			log.Println(err)
		}
	}
}
