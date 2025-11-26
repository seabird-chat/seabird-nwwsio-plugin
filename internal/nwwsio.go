package nwwsio

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"gosrc.io/xmpp/stanza"
)

/*
Documentation:
* https://www.weather.gov/nwws/configuration
* https://www.weather.gov/tg/head

Example Message Format:
<message to='enduser@server/laptop' type='groupchat' from='nwws@nwws-oi.weather.gov/nwws-oi'>

<body>KARX issues RR8 valid 2013-05-25T02:20:34Z</body>

<html xmlns='http://jabber.org/protocol/xhtml-im'>

<body xmlns='http://www.w3.org/1999/xhtml'>KARX issues RR8 valid 2013-05-25T02:20:34Z</body>

</html>

<x xmlns='nwws-oi' cccc='KARX' ttaaii='SRUS83' issue='2013-05-25T02:20:34Z' awipsid='RR8ARX' id='10313.6'>

111

# SRUS83 KARX 250220

# RR8ARX

:

: AUTOMATED GAUGE DATA COLLECTED FROM IOWA FLOOD CENTER

:

.A CDGI4 20130524 C DH2100/HGIRP 2.63 : MORGAN CREEK NEAR CEDAR RAPIDS

</x>

</message>
*/

type NWWSOIMessageXExtension struct {
	stanza.MsgExtension
	XMLName xml.Name `xml:"nwws-oi x"`
	Text    string   `xml:",chardata"`
	// Four character issuing center
	Cccc string `xml:"cccc,attr"`
	// The six character WMO product ID - https://community.wmo.int/en/data-designators-t1t2aia2ii-cccc
	Ttaaii string `xml:"ttaaii,attr"`
	// ISO_8601 datetime in UTC
	Issue string `xml:"issue,attr"`
	// The six character AWIPS ID, sometimes called AFOS PIL.
	AwipsID string `xml:"awipsid,attr"`
	// The id attribute on the <x> stanza is meant to help clients know if they
	// are missing any products as they parse the stream.  The id contains two
	// values loaded up into one and they are separated by a period. The first
	// number is the UNIX process ID on the system running the ingest process.
	// The second number is a simple incremented sequence number for the product.
	ID string `xml:"id,attr"`
}

// GetSequenceID returns the process name and the message sequenceID
// process_name is the UNIX process ID on the system running the ingest process.
// sequenceID is a simple incremented sequence number for the product.
func (n *NWWSOIMessageXExtension) GetSequenceID() (processName string, sequenceID int, err error) {
	splitID := strings.Split(n.ID, ".")
	if len(splitID) != 2 {
		return "", 0, fmt.Errorf("Failed to parse AWIPS ID (%s): %w", n.ID, err)
	}

	processName = splitID[0]
	sequenceID, err = strconv.Atoi(splitID[1])
	if err != nil {
		return "", 0, fmt.Errorf("Failed to parse AWIPS ID (%s): %w", n.ID, err)
	}

	return processName, sequenceID, nil
}

// See https://community.wmo.int/en/table-1 and https://community.wmo.int/en/table-b1
/*
| T1 | Data Type                                       | T2  | A1      | A2      | ii   | Priority  |
|----|--------------------------------------------------|-----|---------|---------|------|-----------|
| A  | Analyses                                         | B1  | C1      | C1      | **   | 3         |
| B  | Addressed message                                | *** | ***     | ***     | ***  | 1/2/4*     |
| C  | Climatic data                                    | B1  | C1      | C1      | **   | 4         |
| D  | Grid point information (GRID)                    | B2  | C3      | C4      | D2   | 3         |
| E  | Satellite imagery                                | B5  | C1      | C1      | **   | 3         |
| F  | Forecast                                         | B1  | C1      | C1      | **   | 3         |
| G  | Grid point information (GRID)                    | B2  | C3      | C4      | D2   | 3         |
| H  | Grid point information (GRIB)                    | B2  | C3      | C4      | D2   | 3         |
| I  | Observational data (Binary coded) - BUFR         | B3  | C6      | C3      | **   | 2         |
| J  | Forecast information (Binary coded) - BUFR       | B3  | C6      | C4      | D2   | 3         |
| K  | CREX                                             | C7  | C7      | C3      | **   | 2         |
| L  | Aviation information in XML                      | B7  | C1      | C1      | *    | 1/2/3     |
| M  | -                                                |     |         |         |      |           |
| N  | Notices                                          | B1  | C1      | C1      | **   | 4         |
| O  | Oceanographic information (GRIB)                 | B4  | C3      | C4      | D1   | 3         |
| P  | Pictorial information (Binary coded)             | B6  | C3      | C4      | D2   | 3         |
| Q  | Pictorial information regional (Binary coded)    | B6  | C3      | C5      | D2   | 3         |
| R  | -                                                |     |         |         |      |           |
| S  | Surface data                                     | B1  | C1/C2   | C1/C2   | **   | 2/4*      |
| T  | Satellite data                                   | B1  | C3      | C4      | **   | 2         |
| U  | Upper air data                                   | B1  | C1/C2   | C1/C2   | **   | 2         |
| V  | National data                                    | (1) | C1      | C1      | **   | (2)       |
| W  | Warnings                                         | B1  | C1      | C1      | **   | 1         |
| X  | Common Alert Protocol (CAP) messages             |     |         |         |      |           |
| Y  | GRIB regional use                                | B2  | C3      | C5      | D2   | 3         |
| Z  | -                                                |     |         |         |      |           |
*/
type DataEntry struct {
	T1       string
	DataType string
	T2       string
	A1       string
	A2       string
	II       string
	Priority []PriorityLevel
}

type PriorityLevel int

const (
	Priority1 PriorityLevel = 1 // Service messages
	Priority2 PriorityLevel = 2 // Data and request messages
	Priority3 PriorityLevel = 3 // Seismic waveform data (T1T2 = SY)
	Priority4 PriorityLevel = 4 // Administrative messages
)

var PriorityDescriptions = map[PriorityLevel]string{
	Priority1: "Service messages",
	Priority2: "Data and request messages",
	Priority3: "Seismic waveform data (T1T2 = SY)",
	Priority4: "Administrative messages",
}

// https://community.wmo.int/en/table-1
var DataTable = []DataEntry{
	{"A", "Analyses", "B1", "C1", "C1", "**", []PriorityLevel{Priority3}},
	{"B", "Addressed message", "***", "***", "***", "***", []PriorityLevel{Priority1, Priority2, Priority4}},
	{"C", "Climatic data", "B1", "C1", "C1", "**", []PriorityLevel{Priority4}},
	{"D", "Grid point information (GRID)", "B2", "C3", "C4", "D2", []PriorityLevel{Priority3}},
	{"E", "Satellite imagery", "B5", "C1", "C1", "**", []PriorityLevel{Priority3}},
	{"F", "Forecast", "B1", "C1", "C1", "**", []PriorityLevel{Priority3}},
	{"G", "Grid point information (GRID)", "B2", "C3", "C4", "D2", []PriorityLevel{Priority3}},
	{"H", "Grid point information (GRIB)", "B2", "C3", "C4", "D2", []PriorityLevel{Priority3}},
	{"I", "Observational data (Binary coded) - BUFR", "B3", "C6", "C3", "**", []PriorityLevel{Priority2}},
	{"J", "Forecast information (Binary coded) - BUFR", "B3", "C6", "C4", "D2", []PriorityLevel{Priority3}},
	{"K", "CREX", "C7", "C7", "C3", "**", []PriorityLevel{Priority2}},
	{"L", "Aviation information in XML", "B7", "C1", "C1", "*", []PriorityLevel{Priority1, Priority2, Priority3}},
	{"M", "-", "", "", "", "", nil},
	{"N", "Notices", "B1", "C1", "C1", "**", []PriorityLevel{Priority4}},
	{"O", "Oceanographic information (GRIB)", "B4", "C3", "C4", "D1", []PriorityLevel{Priority3}},
	{"P", "Pictorial information (Binary coded)", "B6", "C3", "C4", "D2", []PriorityLevel{Priority3}},
	{"Q", "Pictorial information regional (Binary coded)", "B6", "C3", "C5", "D2", []PriorityLevel{Priority3}},
	{"R", "-", "", "", "", "", nil},
	{"S", "Surface data", "B1", "C1/C2", "C1/C2", "**", []PriorityLevel{Priority2, Priority4}},
	{"T", "Satellite data", "B1", "C3", "C4", "**", []PriorityLevel{Priority2}},
	{"U", "Upper air data", "B1", "C1/C2", "C1/C2", "**", []PriorityLevel{Priority2}},
	{"V", "National data", "(1)", "C1", "C1", "**", nil}, // "(2)" not modeled
	{"W", "Warnings", "B1", "C1", "C1", "**", []PriorityLevel{Priority1}},
	{"X", "Common Alert Protocol (CAP) messages", "", "", "", "", nil},
	{"Y", "GRIB regional use", "B2", "C3", "C5", "D2", []PriorityLevel{Priority3}},
	{"Z", "-", "", "", "", "", nil},
}

type WMOProductID struct {
	T1 string
	T2 string
	A1 string
	A2 string
	II string
}

func (n *NWWSOIMessageXExtension) ParseTtaaii() (*WMOProductID, error) {
	if len(n.Ttaaii) != 6 {
		return nil, fmt.Errorf("invalid Ttaaii length: expected 6, got %d", len(n.Ttaaii))
	}

	return &WMOProductID{
		T1: string(n.Ttaaii[0]),
		T2: string(n.Ttaaii[1]),
		A1: string(n.Ttaaii[2]),
		A2: string(n.Ttaaii[3]),
		II: n.Ttaaii[4:6],
	}, nil
}

func (w *WMOProductID) GetDataType() string {
	for _, entry := range DataTable {
		if entry.T1 == w.T1 {
			return entry.DataType
		}
	}
	return "Unknown"
}

// AWIPSProductID represents the parsed AWIPS identifier (NNNxxx)
type AWIPSProductID struct {
	NNN string // 3-character product category
	XXX string // 1-3 character geographic designator
}

// ProductInfo contains metadata about a weather product
type ProductInfo struct {
	Name     string // Full product name
	Category string // Product category (Forecast, Observation, Warning, etc.)
}

// CommonProducts maps AWIPS product abbreviations to their metadata
var CommonProducts = map[string]ProductInfo{
	// Forecasts
	"AFD": {Name: "Area Forecast Discussion", Category: "Forecast"},
	"ZFP": {Name: "Zone Forecast Product", Category: "Forecast"},
	"NOW": {Name: "Short Term Forecast", Category: "Forecast"},
	"FWF": {Name: "Fire Weather Forecast", Category: "Fire Weather"},
	"FWS": {Name: "Fire Weather Outlook", Category: "Fire Weather"},
	"PFM": {Name: "Point Forecast Matrices", Category: "Forecast"},
	"SFT": {Name: "Tabular State Forecast", Category: "Forecast"},

	// Aviation
	"TAF": {Name: "Terminal Aerodrome Forecast", Category: "Aviation"},
	"MET": {Name: "Routine Aviation Weather Report", Category: "Aviation"},
	"SAO": {Name: "Surface Aviation Observation", Category: "Aviation"},
	"FT":  {Name: "Winds/Temps Aloft Forecast", Category: "Aviation"},
	"AVG": {Name: "Aviation Gridded Forecast", Category: "Aviation"},
	"VFT": {Name: "Terminal Forecast Tables", Category: "Aviation"},

	// Warnings & Watches
	"TOR": {Name: "Tornado Warning", Category: "Warning"},
	"SVR": {Name: "Severe Thunderstorm Warning", Category: "Warning"},
	"FFW": {Name: "Flash Flood Warning", Category: "Warning"},
	"FFA": {Name: "Flash Flood Watch", Category: "Watch"},
	"SVS": {Name: "Severe Weather Statement", Category: "Statement"},
	"SPS": {Name: "Special Weather Statement", Category: "Statement"},
	"WSW": {Name: "Winter Storm Warning", Category: "Warning"},
	"WWA": {Name: "Watch Warning Advisory", Category: "Summary"},
	"CFW": {Name: "Coastal Flood Warning", Category: "Warning"},
	"FLW": {Name: "Flood Warning", Category: "Warning"},
	"WAR": {Name: "Space Weather Warning", Category: "Warning"},

	// Marine
	"MWS": {Name: "Marine Weather Statement", Category: "Marine"},
	"MWW": {Name: "Marine Weather Warning", Category: "Marine"},
	"OFF": {Name: "Offshore Forecast", Category: "Marine"},
	"CWF": {Name: "Coastal Waters Forecast", Category: "Marine"},

	// Hydrology
	"RRM": {Name: "Rainfall Storm Total", Category: "Hydrology"},
	"RR":  {Name: "Hydrologic Data", Category: "Hydrology"},
	"RR1": {Name: "Hydrologic Data (1-hour)", Category: "Hydrology"},
	"RR2": {Name: "Hydrologic Data (2-hour)", Category: "Hydrology"},
	"RR3": {Name: "Hydrologic Data (3-hour)", Category: "Hydrology"},
	"RR4": {Name: "Hydrologic Data (4-hour)", Category: "Hydrology"},
	"RR5": {Name: "Hydrologic Data (5-hour)", Category: "Hydrology"},
	"RR6": {Name: "Hydrologic Data (6-hour)", Category: "Hydrology"},
	"RR7": {Name: "Hydrologic Data (7-hour)", Category: "Hydrology"},
	"RR8": {Name: "Hydrologic Data (8-hour)", Category: "Hydrology"},
	"RR9": {Name: "Hydrologic Data (9-hour)", Category: "Hydrology"},
	"FLS": {Name: "Flood Statement", Category: "Hydrology"},
	"HML": {Name: "Hydrologic Monitoring Statement", Category: "Hydrology"},

	// Climate & Observations
	"CLI": {Name: "Daily Climate Report", Category: "Climate"},
	"RTP": {Name: "Regional Temperature/Precipitation", Category: "Climate"},
	"RER": {Name: "Record Event Report", Category: "Climate"},
	"LSR": {Name: "Local Storm Report", Category: "Observation"},
	"RWR": {Name: "Regional Weather Roundup", Category: "Summary"},

	// Public Information
	"PNS": {Name: "Public Information Statement", Category: "Public Info"},
	"HWO": {Name: "Hazardous Weather Outlook", Category: "Outlook"},
	"RWS": {Name: "Regional Weather Summary", Category: "Summary"},

	// Administrative & Technical
	"CAP": {Name: "Common Alerting Protocol", Category: "Administrative"},
	"AFM": {Name: "Area Forecast Matrices", Category: "Forecast"},
	"FHM": {Name: "Forecast Hydrometeorological", Category: "Forecast"},
	"NSH": {Name: "Nearshore Marine Forecast", Category: "Marine"},
	"FWM": {Name: "Fire Weather Matrix", Category: "Fire Weather"},
}

// ParseAwipsID parses the AWIPS identifier into its components
func (n *NWWSOIMessageXExtension) ParseAwipsID() (*AWIPSProductID, error) {
	// Trim whitespace that may come from XML parsing
	awipsID := strings.TrimSpace(n.AwipsID)

	if len(awipsID) < 3 {
		return nil, fmt.Errorf("invalid AWIPS ID length: expected at least 3, got %d", len(awipsID))
	}

	// AWIPS ID format is NNNxxx where NNN is always 3 chars
	// and xxx is 1-3 chars (geographic designator)
	return &AWIPSProductID{
		NNN: awipsID[:3],
		XXX: awipsID[3:],
	}, nil
}

// GetProductInfo returns detailed product information if available
func (a *AWIPSProductID) GetProductInfo() (ProductInfo, bool) {
	info, found := CommonProducts[a.NNN]
	return info, found
}

// GetProductName returns a friendly name for the product
func (a *AWIPSProductID) GetProductName() string {
	if info, found := a.GetProductInfo(); found {
		return info.Name
	}
	return a.NNN // Return the abbreviation if not found
}

// GetProductCategory returns the product category
func (a *AWIPSProductID) GetProductCategory() string {
	if info, found := a.GetProductInfo(); found {
		return info.Category
	}
	return "Unknown"
}

func init() {
	stanza.TypeRegistry.MapExtension(stanza.PKTMessage, xml.Name{Space: "nwws-oi", Local: "x"}, NWWSOIMessageXExtension{})
}
