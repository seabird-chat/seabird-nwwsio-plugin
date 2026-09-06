package nwwsio

import (
	"encoding/xml"
	"strings"
)

// CAP v1.2 (https://docs.oasis-open.org/emergency/cap/v1.2/CAP-v1.2-os.pdf)
// as profiled by the NWS: https://www.weather.gov/media/alert/CAP_v12_guide_05-16-2017.pdf

type Alert struct {
	XMLName     xml.Name `xml:"alert"`
	Xmlns       string   `xml:"xmlns,attr"`
	Identifier  string   `xml:"identifier"`
	Sender      string   `xml:"sender"`
	Sent        string   `xml:"sent"`
	Status      string   `xml:"status"`  // Actual, Exercise, System, Test, Draft
	MsgType     string   `xml:"msgType"` // Alert, Update, Cancel, Ack, Error
	Source      string   `xml:"source"`
	Scope       string   `xml:"scope"`
	Restriction string   `xml:"restriction"`
	Addresses   string   `xml:"addresses"`
	Code        []string `xml:"code"`
	Note        string   `xml:"note"`
	References  string   `xml:"references"`
	Incidents   string   `xml:"incidents"`
	Info        []Info   `xml:"info"`
}

type Info struct {
	Language     string      `xml:"language"`
	Category     []string    `xml:"category"`
	Event        string      `xml:"event"`
	ResponseType []string    `xml:"responseType"`
	Urgency      string      `xml:"urgency"`   // Immediate, Expected, Future, Past, Unknown
	Severity     string      `xml:"severity"`  // Extreme, Severe, Moderate, Minor, Unknown
	Certainty    string      `xml:"certainty"` // Observed, Likely, Possible, Unlikely, Unknown
	Audience     string      `xml:"audience"`
	EventCode    []ValuePair `xml:"eventCode"`
	Effective    string      `xml:"effective"`
	Onset        string      `xml:"onset"`
	Expires      string      `xml:"expires"`
	SenderName   string      `xml:"senderName"`
	Headline     string      `xml:"headline"`
	Description  string      `xml:"description"`
	Instruction  string      `xml:"instruction"`
	Web          string      `xml:"web"`
	Contact      string      `xml:"contact"`
	Parameter    []ValuePair `xml:"parameter"`
	Resource     []Resource  `xml:"resource"`
	Area         []Area      `xml:"area"`
}

type Area struct {
	AreaDesc string      `xml:"areaDesc"`
	Polygon  []string    `xml:"polygon"`
	Circle   []string    `xml:"circle"`
	Geocode  []ValuePair `xml:"geocode"` // SAME and UGC, one element per code
	Altitude string      `xml:"altitude"`
	Ceiling  string      `xml:"ceiling"`
}

type ValuePair struct {
	ValueName string `xml:"valueName"`
	Value     string `xml:"value"`
}

type Resource struct {
	ResourceDesc string `xml:"resourceDesc"`
	MimeType     string `xml:"mimeType"`
	Size         int    `xml:"size"`
	URI          string `xml:"uri"`
	DerefURI     string `xml:"derefUri"`
	Digest       string `xml:"digest"`
}

// ParseCAP parses the CAP document in a product body, returning nil, nil
// when the text holds no alert element.
func ParseCAP(xmlText string) (*Alert, error) {
	xmlText = strings.TrimSpace(xmlText)
	if !strings.Contains(xmlText, "<alert") {
		return nil, nil
	}
	var alert Alert
	if err := xml.Unmarshal([]byte(xmlText), &alert); err != nil {
		return nil, err
	}
	return &alert, nil
}

func (a *Alert) GetPrimaryInfo() *Info {
	if len(a.Info) > 0 {
		return &a.Info[0]
	}
	return nil
}

// AllSAMECodes returns the sorted, deduplicated SAME geocodes across every
// area of every info block.
func (a *Alert) AllSAMECodes() []string {
	var codes []string
	for _, info := range a.Info {
		for _, area := range info.Area {
			codes = append(codes, area.GetAllSAMECodes()...)
		}
	}
	return sortedUnique(codes)
}

func (a *Area) GetAllSAMECodes() []string {
	var values []string
	for _, code := range a.Geocode {
		if code.ValueName == "SAME" {
			values = append(values, strings.Fields(code.Value)...)
		}
	}
	return values
}
