package main

import (
	"encoding/csv"
	"os"
	"strings"
)

func main() {
	// Read cookies CSV
	cookiesFile, err := os.Open("deny_all_vendors.csv")
	if err != nil {
		panic(err)
	}
	defer cookiesFile.Close()

	cookiesReader := csv.NewReader(cookiesFile)
	cookies, err := cookiesReader.ReadAll()
	if err != nil {
		panic(err)
	}

	// Read vendors CSV
	vendorsFile, err := os.Open("gvl_data.csv")
	if err != nil {
		panic(err)
	}
	defer vendorsFile.Close()

	vendorsReader := csv.NewReader(vendorsFile)
	vendors, err := vendorsReader.ReadAll()
	if err != nil {
		panic(err)
	}

	// Create results CSV for matched cookies
	matchedFile, err := os.Create("matched_results.csv")
	if err != nil {
		panic(err)
	}
	defer matchedFile.Close()

	matchedWriter := csv.NewWriter(matchedFile)

	// Create results CSV for unmatched cookies
	unmatchedFile, err := os.Create("unmatched_results.csv")
	if err != nil {
		panic(err)
	}
	defer unmatchedFile.Close()

	unmatchedWriter := csv.NewWriter(unmatchedFile)

	// Create results CSV for partially matched cookies
	partialMatchFile, err := os.Create("partial_match_results.csv")
	if err != nil {
		panic(err)
	}
	defer partialMatchFile.Close()

	partialMatchWriter := csv.NewWriter(partialMatchFile)

	// Iterate through cookies
	for _, cookie := range cookies {
		cookieDomain := strings.ReplaceAll(cookie[1], " ", "")
		cookieName := strings.ReplaceAll(cookie[2], " ", "")
		foundMatch := false
		partialMatch := false

		var partialMatchVendor []string

		// Iterate through vendors
		for _, vendor := range vendors {

			vendorCookieDomains := strings.Split(strings.ReplaceAll(vendor[4], " ", ""), ";")
			vendorGeneralDomains := strings.Split(strings.ReplaceAll(vendor[7], " ", ""), ";")
			vendorDomains := append(vendorCookieDomains, vendorGeneralDomains...)
			vendorCookies := strings.Split(strings.ReplaceAll(vendor[5], " ", ""), ";")
			cookiePurposes := strings.Split(vendor[6], ";")

			domainMatched := false

			// Iterate through vendor domains
			for _, vendorDomain := range vendorDomains {
				if domainMatches(cookieDomain, vendorDomain) {
					domainMatched = true
					break
				}
			}

			if domainMatched {
				partialMatch = true
				partialMatchVendor = vendor
				// Iterate through vendor cookies
				for i, vendorCookie := range vendorCookies {
					if cookieName == vendorCookie {
						foundMatch = true
						// Write matched result
						row := []string{cookie[0], vendor[0], vendor[1], vendor[2], cookieName, cookieDomain, cookiePurposes[i]}
						err = matchedWriter.Write(row)
						if err != nil {
							panic(err)
						}
						break
					}
				}

			}

			if foundMatch {
				break
			}
		}

		if !foundMatch {

			if partialMatch {
				// Write partially matched cookie to partial_match_results.csv
				row := []string{cookie[0], partialMatchVendor[0], partialMatchVendor[1], partialMatchVendor[2], cookieName, cookieDomain}
				err = partialMatchWriter.Write(row)
				if err != nil {
					panic(err)
				}
			} else {
				// Write unmatched cookie to unmatched_results.csv
				row := []string{cookie[0], cookieName, cookieDomain}
				err = unmatchedWriter.Write(row)
				if err != nil {
					panic(err)
				}
			}
		}
	}

	matchedWriter.Flush()
	unmatchedWriter.Flush()
	partialMatchWriter.Flush()
}

func domainMatches(cookieDomain, vendorDomain string) bool {

	vendorSplit := strings.Split(vendorDomain, ".")
	cookieSplit := strings.Split(cookieDomain, ".")
	matchCount := 0

	for _, v_seg := range vendorSplit {
		for _, c_seg := range cookieSplit {
			if v_seg == c_seg {
				matchCount++
			}
		}
	}

	return matchCount > 1

}
