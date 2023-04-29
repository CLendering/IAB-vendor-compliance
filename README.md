# IAB-vendor-compliance
The scripts provided in this repository can be used to assess compliance of both CMP's and Advertising vendors with IAB Europe's TCFv2.0.

## CMP compliance check:
1. Compile a list of domains that implement the TCFv2.0 using [tcf-crawler.py](tcf-availability-crawler/tcf-crawler.py)
2. For each domain found in 1., inject a custom consent string and evaluate CMP compliance using [inject-custom-consent.go](cmp-compliance-check/inject-custom-consent.go)

## Adtech-vendor compliance check:
1. Compile a list of domains that implement the TCFv2.0 using [tcf-crawler.py](tcf-availability-crawler/tcf-crawler.py)
2. For each custom consent configuration, extract all third party cookies set accross all domains using [extract-third-party-cookies.go](vendor-compliance-check/extract-third-party-cookies.go)
3. Use [gvl-to-csv.go](cross-reference-gvl/gvl-to-csv.go) to extract the different vendors/cookie purposes from the Global Vendor List (GVL) and organize the data in a CSV file.
4. Use [reference-gvl.go](vendor-compliance-check/cross-reference-gvl//reference-gvl.go) to classify all third party cookies set in 2.
