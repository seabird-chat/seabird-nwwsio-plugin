package nwwsio

import (
	_ "embed"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// A SAME code is six digits: a leading 0, the two-digit state FIPS code and
// the three-digit county FIPS code. Marine zones use a two-digit "pseudo
// state" code for the ocean basin or lake instead of a state.
//
// Data sources:
//   data/same_codes.txt        https://www.weather.gov/source/nwr/SameCode.txt, verbatim
//   data/zone_county.txt       https://www.weather.gov/gis/ZoneCounty (bp16ap26.dbx),
//                              reduced to STATE|ZONE|FIPS, plus the bp18mr25.dbx rows
//                              for the 56 zone ids retired since: roundups and tabular
//                              forecasts kept using them months after the change.
//   data/fire_zone_county.txt  NWS publishes no correlation for fire weather zones,
//                              so this is fz16ap26.zip (https://www.weather.gov/gis/FireZones)
//                              intersected with c_16ap26.zip (https://www.weather.gov/gis/Counties);
//                              a county is kept when the overlap is at least 2% of the
//                              smaller of the two areas. Same STATE|ZONE|FIPS layout.
//                              Checked against bp16ap26: 2992 of the 3016 zone ids present
//                              in both files get identical county sets.

//go:embed data/same_codes.txt
var sameCodesData string

//go:embed data/zone_county.txt
var zoneCountyData string

//go:embed data/fire_zone_county.txt
var fireZoneCountyData string

// Marine zone UGC prefix to SAME "pseudo state", https://www.weather.gov/marine/wxradio
var marinePseudoState = map[string]string{
	"AN": "73", // Atlantic, Canadian border to Currituck Beach Light NC
	"AM": "75", // Atlantic south of Currituck Beach Light, incl. Caribbean
	"GM": "77", // Gulf of Mexico
	"PZ": "57", // Pacific, US West Coast
	"PK": "58", // Alaska waters
	"PH": "59", // Hawaii waters
	"PM": "65", // Mariana Islands waters
	"PS": "61", // American Samoa waters
	"LS": "91", // Lake Superior
	"LM": "92", // Lake Michigan
	"LH": "93", // Lake Huron
	"LC": "94", // Lake St. Clair
	"LE": "96", // Lake Erie
	"LO": "97", // Lake Ontario
	"SL": "98", // St. Lawrence River
}

type sameTables struct {
	countyName map[string]string   // SAME code -> "Duval, FL"
	stateFIPS  map[string]string   // "FL" -> "12"
	stateCodes map[string][]string // "FL" -> every county SAME code in the state
	zoneCodes  map[string][]string // "TNZ088" -> SAME codes of the counties in the zone
	fireCodes  map[string][]string // same, for fire weather zones
}

var (
	tablesOnce sync.Once
	tables     *sameTables
)

func loadTables() *sameTables {
	tablesOnce.Do(func() {
		tables = parseTables(sameCodesData, zoneCountyData, fireZoneCountyData)
	})
	return tables
}

func parseTables(sameCodes, zoneCounty, fireZoneCounty string) *sameTables {
	t := &sameTables{
		countyName: make(map[string]string),
		stateFIPS:  make(map[string]string),
		stateCodes: make(map[string][]string),
		zoneCodes:  parseZoneCounties(zoneCounty),
		fireCodes:  parseZoneCounties(fireZoneCounty),
	}
	for _, line := range strings.Split(sameCodes, "\n") {
		parts := strings.Split(strings.TrimSpace(line), ",")
		if len(parts) != 3 || len(parts[0]) != 6 {
			continue
		}
		code, name, state := parts[0], strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
		t.countyName[code] = name + ", " + state
		t.stateFIPS[state] = code[1:3]
		t.stateCodes[state] = append(t.stateCodes[state], code)
	}
	for k, v := range t.stateCodes {
		t.stateCodes[k] = sortedUnique(v)
	}
	return t
}

func parseZoneCounties(data string) map[string][]string {
	codes := make(map[string][]string)
	for _, line := range strings.Split(data, "\n") {
		parts := strings.Split(strings.TrimSpace(line), "|")
		if len(parts) != 3 || len(parts[2]) != 5 {
			continue
		}
		key := parts[0] + "Z" + parts[1]
		codes[key] = append(codes[key], "0"+parts[2])
	}
	for k, v := range codes {
		codes[k] = sortedUnique(v)
	}
	return codes
}

var sameCodeRe = regexp.MustCompile(`^\d{6}$`)

func ValidateSAMECode(code string) error {
	if !sameCodeRe.MatchString(code) {
		return fmt.Errorf("SAME code must be 6 digits (e.g., 012031)")
	}
	return nil
}

// SAMECountyName returns "County, ST" for a listed county code; marine codes
// are not listed.
func SAMECountyName(code string) (string, bool) {
	name, ok := loadTables().countyName[code]
	return name, ok
}

func StateFIPS(abbr string) (string, bool) {
	fips, ok := loadTables().stateFIPS[abbr]
	return fips, ok
}

// UGCToSAME maps one UGC (SSCnnn or SSZnnn) to the SAME codes it covers: a
// county to one code, a land zone to every county in it, a marine zone to
// its marine code, and ALL/000 to the whole state. Public and fire weather
// zones share the SSZnnn space; the public correlation wins when both list
// the same id.
func UGCToSAME(ugc string) []string {
	if len(ugc) != 6 {
		return nil
	}
	t := loadTables()
	state, form, nnn := ugc[0:2], ugc[2], ugc[3:6]
	if nnn == "ALL" || nnn == "000" {
		return append([]string(nil), t.stateCodes[state]...)
	}
	switch form {
	case 'C':
		fips, ok := t.stateFIPS[state]
		if !ok {
			return nil
		}
		return []string{"0" + fips + nnn}
	case 'Z':
		if pseudo, ok := marinePseudoState[state]; ok {
			return []string{"0" + pseudo + nnn}
		}
		if codes, ok := t.zoneCodes[ugc]; ok {
			return append([]string(nil), codes...)
		}
		return append([]string(nil), t.fireCodes[ugc]...)
	}
	return nil
}

var (
	ugcStartRe = regexp.MustCompile(`^[A-Z]{2}[CZ](\d{3}|ALL)[->]`)
	ugcContRe  = regexp.MustCompile(`^([A-Z0-9>-]+-|\d{6})$`)
	// Roundups (RWR) omit the dash after the purge time.
	ugcEndRe    = regexp.MustCompile(`\d{6}-?$`)
	ugcExpiryRe = regexp.MustCompile(`^\d{6}$`)
	ugcGroupRe  = regexp.MustCompile(`^([A-Z]{2}[CZ])(.*)$`)
)

// ParseUGC returns every code named by the UGC blocks of a text product,
// in order, with ranges and state-prefix elision expanded (NWSI 10-1702:
// "SSFNNN-NNN>NNN-SSFNNN-DDHHMM-", possibly wrapped over lines ending in "-").
func ParseUGC(text string) []string {
	var codes []string
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !ugcStartRe.MatchString(line) {
			continue
		}
		block := line
		for !ugcEndRe.MatchString(block) && i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			if next == "" {
				// NWWS-OI delivers products with a blank line after every line.
				i++
				continue
			}
			if !ugcContRe.MatchString(next) {
				break
			}
			i++
			block += next
		}
		if !ugcEndRe.MatchString(block) {
			continue
		}
		codes = append(codes, expandUGCBlock(block)...)
	}
	return codes
}

func expandUGCBlock(block string) []string {
	var codes []string
	prefix := ""
	for _, token := range strings.Split(strings.TrimSuffix(block, "-"), "-") {
		if ugcExpiryRe.MatchString(token) {
			break
		}
		group := token
		if m := ugcGroupRe.FindStringSubmatch(token); m != nil {
			prefix, group = m[1], m[2]
		}
		if prefix == "" {
			continue
		}
		if from, to, ok := strings.Cut(group, ">"); ok {
			a, errA := strconv.Atoi(from)
			b, errB := strconv.Atoi(to)
			if errA != nil || errB != nil || b < a {
				continue
			}
			for n := a; n <= b; n++ {
				codes = append(codes, fmt.Sprintf("%s%03d", prefix, n))
			}
			continue
		}
		codes = append(codes, prefix+group)
	}
	return codes
}

func SAMECodesForProduct(text string) []string {
	var codes []string
	for _, ugc := range ParseUGC(text) {
		codes = append(codes, UGCToSAME(ugc)...)
	}
	return sortedUnique(codes)
}

func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
