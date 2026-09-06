package nwwsio

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"gosrc.io/xmpp/stanza"
)

// NWWSOIMessageXExtension is the <x xmlns='nwws-oi'> payload on every product
// message in the NWWS-OI room. See https://www.weather.gov/nwws/configuration
// and https://www.weather.gov/tg/head.
type NWWSOIMessageXExtension struct {
	stanza.MsgExtension
	XMLName xml.Name `xml:"nwws-oi x"`
	Text    string   `xml:",chardata"`
	Cccc    string   `xml:"cccc,attr"`    // issuing center
	Ttaaii  string   `xml:"ttaaii,attr"`  // WMO product ID
	Issue   string   `xml:"issue,attr"`   // ISO 8601 UTC
	AwipsID string   `xml:"awipsid,attr"` // AWIPS ID / AFOS PIL
	ID      string   `xml:"id,attr"`      // "<ingest process ID>.<sequence number>"
}

func (n *NWWSOIMessageXExtension) GetSequenceID() (processName string, sequenceID int, err error) {
	splitID := strings.Split(n.ID, ".")
	if len(splitID) != 2 {
		return "", 0, fmt.Errorf("failed to parse message ID (%s): expected format 'processID.sequenceID'", n.ID)
	}
	sequenceID, err = strconv.Atoi(splitID[1])
	if err != nil {
		return "", 0, fmt.Errorf("failed to parse message ID (%s): %w", n.ID, err)
	}
	return splitID[0], sequenceID, nil
}

// WMO abbreviated heading data types, https://wmo.int/table-1
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
	Priority1 PriorityLevel = 1
	Priority2 PriorityLevel = 2
	Priority3 PriorityLevel = 3
	Priority4 PriorityLevel = 4
)

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
	{"V", "National data", "(1)", "C1", "C1", "**", nil},
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

// AWIPSProductID is the NNNxxx AWIPS identifier: a 3-character product code
// and a 1-3 character geographic designator.
type AWIPSProductID struct {
	NNN string
	XXX string
}

func (n *NWWSOIMessageXExtension) ParseAwipsID() (*AWIPSProductID, error) {
	awipsID := strings.TrimSpace(n.AwipsID)
	if len(awipsID) < 3 {
		return nil, fmt.Errorf("invalid AWIPS ID length: expected at least 3, got %d", len(awipsID))
	}
	return &AWIPSProductID{NNN: awipsID[:3], XXX: awipsID[3:]}, nil
}

func (a *AWIPSProductID) GetProductInfo() (ProductInfo, bool) {
	info, found := CommonProducts[a.NNN]
	return info, found
}

func (a *AWIPSProductID) GetProductName() string {
	if info, found := a.GetProductInfo(); found {
		return info.Name
	}
	return a.NNN
}

func (a *AWIPSProductID) GetProductCategory() string {
	if info, found := a.GetProductInfo(); found {
		return info.Category
	}
	return "Unknown"
}

func init() {
	stanza.TypeRegistry.MapExtension(stanza.PKTMessage, xml.Name{Space: "nwws-oi", Local: "x"}, NWWSOIMessageXExtension{})
}
