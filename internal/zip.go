package nwwsio

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// data/zip_county.txt is the US Census 2020 ZCTA-to-county relationship file
// (tab20_zcta520_county20_natl.txt) reduced to ZIP|FIPS pairs, keeping a
// county when it holds at least 5% of the ZIP's land area or the largest
// share. ZCTAs approximate USPS ZIPs; PO-box-only ZIPs have none.
//
//go:embed data/zip_county.txt
var zipCountyData string

var (
	zipOnce  sync.Once
	zipCodes map[string][]string // ZIP -> SAME codes
)

func loadZIPTable() map[string][]string {
	zipOnce.Do(func() {
		zipCodes = make(map[string][]string)
		for _, line := range strings.Split(zipCountyData, "\n") {
			zip, fips, ok := strings.Cut(strings.TrimSpace(line), "|")
			if !ok || len(zip) != 5 || len(fips) != 5 {
				continue
			}
			zipCodes[zip] = append(zipCodes[zip], "0"+fips)
		}
		for zip, codes := range zipCodes {
			zipCodes[zip] = sortedUnique(codes)
		}
	})
	return zipCodes
}

var zipRe = regexp.MustCompile(`^(\d{5})(?:-\d{4})?$`)

// NormalizeZIP accepts "48103" or "48103-1680" and returns the 5-digit ZIP.
func NormalizeZIP(zip string) (string, error) {
	m := zipRe.FindStringSubmatch(zip)
	if m == nil {
		return "", fmt.Errorf("ZIP code must be 5 digits or ZIP+4 (e.g., 48103 or 48103-1680)")
	}
	return m[1], nil
}

func ZIPToSAME(zip string) []string {
	return append([]string(nil), loadZIPTable()[zip]...)
}
