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

type VendorList struct {
	Vendors map[string]Vendor `json:"vendors"`
}

type Vendor struct {
	Name                       string `json:"name"`
	ID                         int    `json:"id"`
	DeviceStorageDisclosureUrl string `json:"deviceStorageDisclosureUrl"`
	Purposes                   []int  `json:"purposes"`
}

type DeviceDisclosure struct {
	Disclosures []Disclosure `json:"disclosures"`
	Domains     []Domain     `json:"domains"`
}

type Disclosure struct {
	Identifier    string   `json:"identifier"`
	Type          string   `json:"type"`
	MaxAgeSeconds *int     `json:"maxAgeSeconds"`
	CookieRefresh bool     `json:"cookieRefresh"`
	Domains       []string `json:"domains"`
	Purposes      []int    `json:"purposes"`
}

type Domain struct {
	Domain string `json:"domain"`
	Use    string `json:"use"`
}

func main() {
	// Get the Global Vendor List from the IAB TCF v2.0 URL
	resp, err := http.Get("https://vendor-list.consensu.org/v2/vendor-list.json")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	var vendorList VendorList
	err = json.Unmarshal(body, &vendorList)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// Create the output CSV file
	outputFile, err := os.Create("gvl_data.csv")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer outputFile.Close()

	writer := csv.NewWriter(outputFile)
	defer writer.Flush()

	// Write the header row
	header := []string{"Vendor Name", "Vendor ID", "Purposes", "Device Disclosure URL", "Cookie Domains", "Cookie Names", "Cookie Purposes", "Vendor Domains", "Vendor Uses"}
	err = writer.Write(header)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// Iterate through the vendors in the Global Vendor List
	for _, vendor := range vendorList.Vendors {
		deviceDisclosure, err := fetchDeviceDisclosure(vendor.DeviceStorageDisclosureUrl)
		if err != nil {
			fmt.Println("Error:", err)
			continue
		}
		cookieDomains := []string{}
		cookieIdentifiers := []string{}
		cookiePurposes := []string{}

		vendorDomains := []string{}
		vendorUses := []string{}

		for _, disclosure := range deviceDisclosure.Disclosures {
			if disclosure.Type == "cookie" {
				cookieIdentifiers = append(cookieIdentifiers, disclosure.Identifier)
				cookieDomains = append(cookieDomains, strings.Join(disclosure.Domains, ", "))
				cookiePurposes = append(cookiePurposes, fmt.Sprintf("%v", disclosure.Purposes))
			}
		}

		for _, domain := range deviceDisclosure.Domains {
			vendorDomains = append(vendorDomains, domain.Domain)
			vendorUses = append(vendorUses, domain.Use)
		}

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
		err = writer.Write(row)
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
	}
}

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
