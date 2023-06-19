package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
)

// Constants used in this program
const (
	vendorListURL  = "https://vendor-list.consensu.org/v2/vendor-list.json"
	outputFileName = "gvl_data.csv"
)

// VendorList represents the structure of the vendor list found on  the vendorListURL.
type VendorList struct {
	Vendors map[string]Vendor `json:"vendors"`
}

// Vendor represents the details of a vendor present in the VendorList.
type Vendor struct {
	Name                       string `json:"name"`
	ID                         int    `json:"id"`
	DeviceStorageDisclosureUrl string `json:"deviceStorageDisclosureUrl"`
	Purposes                   []int  `json:"purposes"`
}

// DeviceDisclosure represents the structure of the device disclosure data.
type DeviceDisclosure struct {
	Disclosures []Disclosure `json:"disclosures"`
	Domains     []Domain     `json:"domains"`
}

// Disclosure represents the details of a specific disclosure.
type Disclosure struct {
	Identifier    string   `json:"identifier"`
	Type          string   `json:"type"`
	MaxAgeSeconds *int     `json:"maxAgeSeconds"`
	CookieRefresh bool     `json:"cookieRefresh"`
	Domains       []string `json:"domains"`
	Purposes      []int    `json:"purposes"`
}

// Domain represents the domain related to a vendor.
type Domain struct {
	Domain string `json:"domain"`
	Use    string `json:"use"`
}

// The main function where the program starts
func main() {
	vendorList := fetchVendorList(vendorListURL)
	createVendorCSV(vendorList, outputFileName)
}

// fetchVendorList retrieves the vendor list from the provided URL.
func fetchVendorList(url string) *VendorList {
	resp, err := http.Get(url)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	var vendorList VendorList
	err = json.Unmarshal(body, &vendorList)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	return &vendorList
}

// createVendorCSV creates a CSV file from the provided VendorList data.
func createVendorCSV(vendorList *VendorList, fileName string) {
	outputFile, err := os.Create(fileName)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	defer outputFile.Close()

	writer := csv.NewWriter(outputFile)
	defer writer.Flush()

	writeHeader(writer)

	// Iterate through the vendors in the Global Vendor List
	for _, vendor := range vendorList.Vendors {
		deviceDisclosure, err := fetchDeviceDisclosure(vendor.DeviceStorageDisclosureUrl)
		if err != nil {
			fmt.Println("Error:", err)
			continue
		}

		writeVendor(writer, vendor, deviceDisclosure)
	}
}

// writeHeader writes the header row to the CSV file.
func writeHeader(writer *csv.Writer) {
	header := []string{"Vendor Name", "Vendor ID", "Purposes", "Device Disclosure URL", "Cookie Domains", "Cookie Names", "Cookie Purposes", "Vendor Domains", "Vendor Uses"}
	err := writer.Write(header)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

// writeVendor writes the vendor information to the CSV file.
func writeVendor(writer *csv.Writer, vendor Vendor, deviceDisclosure *DeviceDisclosure) {
	cookieDomains, cookieIdentifiers, cookiePurposes := processDisclosures(deviceDisclosure.Disclosures)
	vendorDomains, vendorUses := processDomains(deviceDisclosure.Domains)

	row := []string{
		vendor.Name,
		fmt.Sprintf("%d", vendor.ID),
		fmt.Sprintf("%v", vendor.Purposes),
		vendor.DeviceStorageDisclosureUrl,
		strings.Join(cookieDomains, "; "),
		strings.Join(cookieIdentifiers, "; "),
		strings.Join(cookiePurposes, "; "),
		strings.Join(vendorDomains, "; "),
		strings.Join(vendorUses, "; "),
	}
	err := writer.Write(row)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

// processDisclosures processes disclosures and returns cookieDomains, cookieIdentifiers, cookiePurposes
func processDisclosures(disclosures []Disclosure) (cookieDomains, cookieIdentifiers, cookiePurposes []string) {
	for _, disclosure := range disclosures {
		if disclosure.Type == "cookie" {
			cookieIdentifiers = append(cookieIdentifiers, disclosure.Identifier)
			cookieDomains = append(cookieDomains, strings.Join(disclosure.Domains, ", "))
			cookiePurposes = append(cookiePurposes, fmt.Sprintf("%v", disclosure.Purposes))
		}
	}
	return
}

// processDomains processes domains and returns vendorDomains, vendorUses
func processDomains(domains []Domain) (vendorDomains, vendorUses []string) {
	for _, domain := range domains {
		vendorDomains = append(vendorDomains, domain.Domain)
		vendorUses = append(vendorUses, domain.Use)
	}
	return
}

// fetchDeviceDisclosure fetches device disclosure from a given URL
func fetchDeviceDisclosure(url string) (*DeviceDisclosure, error) {
	if url == "" {
		return &DeviceDisclosure{}, nil
	}

	// Create a custom HTTP client with a user-agent
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.3")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch device disclosure from %s, status code: %d", url, resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var deviceDisclosure DeviceDisclosure
	err = json.Unmarshal(body, &deviceDisclosure)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal device disclosure JSON from %s: %v", url, err)
	}

	return &deviceDisclosure, nil
}
