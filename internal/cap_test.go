package nwwsio

import (
	"reflect"
	"testing"
)

const capTwoAreas = `<?xml version="1.0" encoding="UTF-8"?>
<alert xmlns="urn:oasis:names:tc:emergency:cap:1.2">
<identifier>test-1</identifier><status>Actual</status><msgType>Alert</msgType>
<info>
 <event>Small Craft Advisory</event>
 <eventCode><valueName>SAME</valueName><value>NWS</value></eventCode>
 <area><areaDesc>Waters A</areaDesc>
  <geocode><valueName>SAME</valueName><value>057455</value></geocode>
  <geocode><valueName>UGC</valueName><value>PZZ455</value></geocode>
 </area>
 <area><areaDesc>Waters B</areaDesc>
  <geocode><valueName>SAME</valueName><value>057475</value></geocode>
  <geocode><valueName>SAME</valueName><value>057470</value></geocode>
 </area>
</info>
<info>
 <language>es-US</language><event>Aviso</event>
 <area><areaDesc>Duval</areaDesc>
  <geocode><valueName>SAME</valueName><value>012031</value></geocode>
 </area>
</info>
</alert>`

// One geocode element per code, across every area and info block; the
// eventCode SAME entry is not a geocode.
func TestAlertAllSAMECodes(t *testing.T) {
	alert, err := ParseCAP(capTwoAreas)
	if err != nil {
		t.Fatalf("ParseCAP: %v", err)
	}
	want := []string{"012031", "057455", "057470", "057475"}
	if got := alert.AllSAMECodes(); !reflect.DeepEqual(got, want) {
		t.Errorf("AllSAMECodes = %v, want %v", got, want)
	}
}
