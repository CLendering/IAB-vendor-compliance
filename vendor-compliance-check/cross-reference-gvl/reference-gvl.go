package main

import (
	"encoding/csv"
	"os"
	"strings"
)

// Define constants for file names
const (
	CookiesCSV          = "deny_all_vendors.csv"
	GvlCSV              = "gvl_data.csv"
	MatchedResultsCSV   = "matched_results.csv"
	UnmatchedResultsCSV = "unmatched_results.csv"
	PartialMatchCSV     = "partial_match_results.csv"
)

func main() {
	cookies := readCSV(CookiesCSV)
	vendors := readCSV(GvlCSV)

	matchedWriter := createCSVWriter(MatchedResultsCSV)
	defer matchedWriter.Flush()

	unmatchedWriter := createCSVWriter(UnmatchedResultsCSV)
	defer unmatchedWriter.Flush()

	partialMatchWriter := createCSVWriter(PartialMatchCSV)
	defer partialMatchWriter.Flush()

	// Iterate through cookies
	for _, cookie := range cookies {
		processCookie(cookie, vendors, matchedWriter, unmatchedWriter, partialMatchWriter)
	}
}

// readCSV reads a CSV file and returns its content.
func readCSV(filename string) [][]string {
	file, err := os.Open(filename)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		panic(err)
	}
	return records
}

// createCSVWriter creates and returns a CSV writer
func createCSVWriter(filename string) *csv.Writer {
	file, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	return csv.NewWriter(file)
}

// processCookie processes a single cookie by checking it against vendors and writing match results.
func processCookie(cookie []string, vendors [][]string, matchedWriter, unmatchedWriter, partialMatchWriter *csv.Writer) {
	cookieDomain := strings.ReplaceAll(cookie[1], " ", "")
	cookieName := strings.ReplaceAll(cookie[2], " ", "")
	foundMatch := false
	partialMatch := false
	var partialMatchVendor []string

	// Iterate through vendors
	for _, vendor := range vendors {
		vendorDomains, vendorCookies := extractVendorData(vendor)

		if domainMatched := findDomainMatch(cookieDomain, vendorDomains); domainMatched {
			partialMatch = true
			partialMatchVendor = vendor
			for i, vendorCookie := range vendorCookies {
				if cookieName == vendorCookie {
					foundMatch = true
					writeMatchResult(matchedWriter, cookie, vendor, cookieName, cookieDomain, i)
					break
				}
			}
		}

		if foundMatch {
			break
		}
	}

	writePartialOrUnmatchedResult(partialMatch, foundMatch, partialMatchWriter, unmatchedWriter, cookie, partialMatchVendor, cookieName, cookieDomain)
}

// extractVendorData extracts vendor data from a row in the GVL data.
func extractVendorData(vendor []string) ([]string, []string) {
	vendorCookieDomains := strings.Split(strings.ReplaceAll(vendor[4], " ", ""), ";")
	vendorGeneralDomains := strings.Split(strings.ReplaceAll(vendor[7], " ", ""), ";")
	vendorDomains := append(vendorCookieDomains, vendorGeneralDomains...)
	vendorCookies := strings.Split(strings.ReplaceAll(vendor[5], " ", ""), ";")
	return vendorDomains, vendorCookies
}

// findDomainMatch checks if a cookie's domain matches a vendor's domains.
func findDomainMatch(cookieDomain string, vendorDomains []string) bool {
	for _, vendorDomain := range vendorDomains {
		if domainMatches(cookieDomain, vendorDomain) {
			return true
		}
	}
	return false
}

// writeMatchResult writes a match result to the matchedWriter.
func writeMatchResult(matchedWriter *csv.Writer, cookie, vendor []string, cookieName, cookieDomain string, i int) {
	row := []string{cookie[0], vendor[0], vendor[1], vendor[2], cookieName, cookieDomain, vendor[6]}
	err := matchedWriter.Write(row)
	if err != nil {
		panic(err)
	}
}

// writePartialOrUnmatchedResult writes the results to the appropriate writer based on the match status.
func writePartialOrUnmatchedResult(partialMatch, foundMatch bool, partialMatchWriter, unmatchedWriter *csv.Writer, cookie, partialMatchVendor []string, cookieName, cookieDomain string) {
	if !foundMatch {
		if partialMatch {
			writePartialMatchResult(partialMatchWriter, cookie, partialMatchVendor, cookieName, cookieDomain)
		} else {
			writeUnmatchedResult(unmatchedWriter, cookie, cookieName, cookieDomain)
		}
	}
}

// writePartialMatchResult writes a partial match result to the partialMatchWriter.
func writePartialMatchResult(partialMatchWriter *csv.Writer, cookie, partialMatchVendor []string, cookieName, cookieDomain string) {
	row := []string{cookie[0], partialMatchVendor[0], partialMatchVendor[1], partialMatchVendor[2], cookieName, cookieDomain}
	err := partialMatchWriter.Write(row)
	if err != nil {
		panic(err)
	}
}

// writeUnmatchedResult writes an unmatched result to the unmatchedWriter.
func writeUnmatchedResult(unmatchedWriter *csv.Writer, cookie []string, cookieName, cookieDomain string) {
	row := []string{cookie[0], cookieName, cookieDomain}
	err := unmatchedWriter.Write(row)
	if err != nil {
		panic(err)
	}
}

// domainMatches checks if the cookie domain matches the vendor domain.
func domainMatches(cookieDomain, vendorDomain string) bool {
	// Split both domains into segments
	vendorSplit := strings.Split(vendorDomain, ".")
	cookieSplit := strings.Split(cookieDomain, ".")

	// Determine which domain is less specific
	var smallerDomain, largerDomain []string
	if len(cookieSplit) < len(vendorSplit) {
		smallerDomain, largerDomain = cookieSplit, vendorSplit
	} else {
		smallerDomain, largerDomain = vendorSplit, cookieSplit
	}

	// Match from right to left (from TLD to subdomain)
	for i := 0; i < len(smallerDomain); i++ {
		// Get the corresponding segments from the end of both split arrays
		smallerSegment := smallerDomain[len(smallerDomain)-1-i]
		largerSegment := largerDomain[len(largerDomain)-1-i]

		// If the segments don't match, return false
		if smallerSegment != largerSegment {
			return false
		}
	}

	// If all segments matched, return true
	return true
}
