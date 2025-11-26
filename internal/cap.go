package nwwsio

import (
	"encoding/xml"
	"strings"
)

// CAP (Common Alerting Protocol) v1.2 structures
// Based on https://docs.oasis-open.org/emergency/cap/v1.2/CAP-v1.2-os.pdf
// and NWS IPAWS profile

// Alert is the root element of a CAP message
type Alert struct {
	XMLName    xml.Name `xml:"alert"`
	Xmlns      string   `xml:"xmlns,attr"`
	Identifier string   `xml:"identifier"`
	Sender     string   `xml:"sender"`
	Sent       string   `xml:"sent"`
	Status     string   `xml:"status"`     // Actual, Exercise, System, Test, Draft
	MsgType    string   `xml:"msgType"`    // Alert, Update, Cancel, Ack, Error
	Source     string   `xml:"source"`     // Optional
	Scope      string   `xml:"scope"`      // Public, Restricted, Private
	Restriction string  `xml:"restriction"` // Optional
	Addresses  string   `xml:"addresses"`  // Optional
	Code       []string `xml:"code"`       // Optional, multiple
	Note       string   `xml:"note"`       // Optional
	References string   `xml:"references"` // Optional
	Incidents  string   `xml:"incidents"`  // Optional
	Info       []Info   `xml:"info"`       // At least one Info block
}

// Info contains the details of the alert
type Info struct {
	Language     string      `xml:"language"`     // Default: en-US
	Category     []string    `xml:"category"`     // Geo, Met, Safety, Security, Rescue, Fire, Health, Env, Transport, Infra, CBRNE, Other
	Event        string      `xml:"event"`        // Event type (e.g., "Tornado Warning")
	ResponseType []string    `xml:"responseType"` // Shelter, Evacuate, Prepare, Execute, Avoid, Monitor, Assess, AllClear, None
	Urgency      string      `xml:"urgency"`      // Immediate, Expected, Future, Past, Unknown
	Severity     string      `xml:"severity"`     // Extreme, Severe, Moderate, Minor, Unknown
	Certainty    string      `xml:"certainty"`    // Observed, Likely, Possible, Unlikely, Unknown
	Audience     string      `xml:"audience"`     // Optional
	EventCode    []ValuePair `xml:"eventCode"`    // Optional, multiple
	Effective    string      `xml:"effective"`    // Optional, ISO 8601 datetime
	Onset        string      `xml:"onset"`        // Optional, ISO 8601 datetime
	Expires      string      `xml:"expires"`      // Optional, ISO 8601 datetime
	SenderName   string      `xml:"senderName"`   // Optional
	Headline     string      `xml:"headline"`     // Optional, brief summary
	Description  string      `xml:"description"`  // Optional, full text
	Instruction  string      `xml:"instruction"`  // Optional, recommended action
	Web          string      `xml:"web"`          // Optional, URL for more info
	Contact      string      `xml:"contact"`      // Optional
	Parameter    []ValuePair `xml:"parameter"`    // Optional, multiple (VTEC, etc.)
	Resource     []Resource  `xml:"resource"`     // Optional, multiple
	Area         []Area      `xml:"area"`         // Optional, multiple
}

// Area describes a geographic area
type Area struct {
	AreaDesc string      `xml:"areaDesc"`        // Human-readable description
	Polygon  []string    `xml:"polygon"`         // Optional, multiple, space-separated lat/lon pairs
	Circle   []string    `xml:"circle"`          // Optional, multiple, "lat,lon radius"
	Geocode  []ValuePair `xml:"geocode"`         // Optional, multiple (SAME, UGC codes)
	Altitude string      `xml:"altitude"`        // Optional
	Ceiling  string      `xml:"ceiling"`         // Optional
}

// ValuePair represents a name-value pair used in parameters and geocodes
type ValuePair struct {
	ValueName string `xml:"valueName"`
	Value     string `xml:"value"`
}

// Resource represents a supplementary digital resource (image, audio, etc.)
type Resource struct {
	ResourceDesc string `xml:"resourceDesc"`
	MimeType     string `xml:"mimeType"`
	Size         int    `xml:"size"`         // Optional, bytes
	URI          string `xml:"uri"`          // Optional
	DerefURI     string `xml:"derefUri"`     // Optional, base64 encoded
	Digest       string `xml:"digest"`       // Optional, SHA-1 hash
}

// ParseCAP attempts to parse a CAP message from XML text
func ParseCAP(xmlText string) (*Alert, error) {
	// Trim any leading/trailing whitespace and check if it looks like CAP
	xmlText = strings.TrimSpace(xmlText)

	if !strings.Contains(xmlText, "<alert") {
		return nil, nil // Not a CAP message
	}

	var alert Alert
	err := xml.Unmarshal([]byte(xmlText), &alert)
	if err != nil {
		return nil, err
	}

	return &alert, nil
}

// GetPrimaryInfo returns the first (usually only) Info block
func (a *Alert) GetPrimaryInfo() *Info {
	if len(a.Info) > 0 {
		return &a.Info[0]
	}
	return nil
}

// GetParameter returns the value of a parameter by name
func (i *Info) GetParameter(name string) string {
	for _, param := range i.Parameter {
		if param.ValueName == name {
			return param.Value
		}
	}
	return ""
}

// GetGeocode returns the value of a geocode by name (e.g., "SAME" or "UGC")
func (a *Area) GetGeocode(name string) string {
	for _, code := range a.Geocode {
		if code.ValueName == name {
			return code.Value
		}
	}
	return ""
}

// GetAllUGCCodes returns all UGC (Universal Geographic Code) values from the area
func (a *Area) GetAllUGCCodes() []string {
	for _, code := range a.Geocode {
		if code.ValueName == "UGC" {
			// UGC codes are space-separated
			return strings.Fields(code.Value)
		}
	}
	return nil
}

// GetAllSAMECodes returns all SAME (Specific Area Message Encoding) codes from the area
func (a *Area) GetAllSAMECodes() []string {
	for _, code := range a.Geocode {
		if code.ValueName == "SAME" {
			// SAME codes are space-separated
			return strings.Fields(code.Value)
		}
	}
	return nil
}
