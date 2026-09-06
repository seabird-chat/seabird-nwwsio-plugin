package client

import (
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	nwwsio "github.com/seabird-chat/seabird-nwwsio-plugin/internal"
	"gosrc.io/xmpp/stanza"
)

const (
	MaxCAPDescriptionLen = 800
	MaxCAPInstructionLen = 200
	MaxRegularProductLen = 1000
)

type productInfo struct {
	productID       *nwwsio.WMOProductID
	productName     string
	productCategory string
	capAlert        *nwwsio.Alert
}

// handleMessage processes one product from the NWWS-IO room: it records it,
// then delivers it to station subscribers and to the SAME and ZIP
// subscribers whose area it covers.
func handleMessage(p stanza.Packet, client *SeabirdClient) {
	msg, ok := p.(stanza.Message)
	if !ok {
		log.Debug().Str("type", fmt.Sprintf("%T", p)).Msg("Ignoring packet")
		return
	}
	var x nwwsio.NWWSOIMessageXExtension
	if !msg.Get(&x) {
		return
	}
	if client.muc != nil {
		client.muc.noteMessage()
	}
	x.AwipsID = strings.TrimSpace(x.AwipsID)

	if processID, sequenceID, err := x.GetSequenceID(); err != nil {
		log.Debug().Err(err).Str("id", x.ID).Msg("Failed to parse sequence ID")
	} else {
		client.checkSequenceGap(processID, sequenceID)
	}

	info, err := parseProductInfo(&x)
	if err != nil {
		log.Warn().Err(err).Str("ttaaii", x.Ttaaii).Msg("Failed to parse product info")
		return
	}
	if client.filterTestMessages && isTestMessage(&x, info.capAlert) {
		log.Debug().Str("cccc", x.Cccc).Str("ttaaii", x.Ttaaii).Str("awipsid", x.AwipsID).Msg("Ignoring test message")
		return
	}

	logProductReceipt(&x, info)
	client.subscriptions.AddRecentMessage(RecentMessage{
		Station:   x.Cccc,
		DataType:  buildDisplayName(info),
		AwipsID:   x.AwipsID,
		Issue:     x.Issue,
		Text:      x.Text,
		Timestamp: time.Now(),
	})

	alertMsg := formatAlertMessage(&x, info)
	deliverToStationSubscribers(client, &x, info, alertMsg)
	deliverToGeoSubscribers(client, &x, info, alertMsg, productSAMECodes(info, x.Text))
}

func (c *SeabirdClient) checkSequenceGap(processID string, sequenceID int) {
	c.sequenceMu.Lock()
	defer c.sequenceMu.Unlock()

	if lastSeq, seen := c.lastSequence[processID]; seen && sequenceID != lastSeq+1 {
		log.Warn().
			Str("process_id", processID).
			Int("expected_seq", lastSeq+1).
			Int("received_seq", sequenceID).
			Int("missed_count", sequenceID-lastSeq-1).
			Msg("Detected missed messages - sequence gap")
	}
	c.lastSequence[processID] = sequenceID
}

func parseProductInfo(x *nwwsio.NWWSOIMessageXExtension) (*productInfo, error) {
	productID, err := x.ParseTtaaii()
	if err != nil {
		return nil, fmt.Errorf("failed to parse WMO product ID: %w", err)
	}

	info := &productInfo{
		productID:       productID,
		productName:     productID.GetDataType(),
		productCategory: "Unknown",
	}

	if awipsID, err := x.ParseAwipsID(); err != nil {
		log.Debug().Err(err).Str("awipsid", x.AwipsID).Str("ttaaii", x.Ttaaii).Msg("Failed to parse AWIPS ID, using WMO type as fallback")
	} else {
		info.productName = awipsID.GetProductName()
		info.productCategory = awipsID.GetProductCategory()
	}

	if isLikelyCAP(productID, x.Text) {
		if capAlert, err := nwwsio.ParseCAP(x.Text); err != nil {
			log.Debug().Err(err).Msg("Failed to parse CAP message")
		} else {
			info.capAlert = capAlert
		}
	}

	return info, nil
}

func isLikelyCAP(productID *nwwsio.WMOProductID, text string) bool {
	return productID.T1 == "X" || strings.Contains(text, "<alert")
}

// isTestMessage matches the NTXX98/99 "THIS IS A TEST MESSAGE" keepalives
// every office sends hourly, TST products, and CAP alerts with a test status.
func isTestMessage(x *nwwsio.NWWSOIMessageXExtension, capAlert *nwwsio.Alert) bool {
	if strings.HasPrefix(x.Ttaaii, "NTXX") || strings.HasPrefix(strings.TrimSpace(x.AwipsID), "TST") {
		return true
	}
	if capAlert != nil {
		switch capAlert.Status {
		case "Test", "Exercise":
			return true
		}
	}
	return false
}

func logProductReceipt(x *nwwsio.NWWSOIMessageXExtension, info *productInfo) {
	event := log.Info().
		Str("cccc", x.Cccc).
		Str("ttaaii", x.Ttaaii).
		Str("wmo_type", info.productID.GetDataType()).
		Str("awipsid", x.AwipsID).
		Str("product", info.productName).
		Str("category", info.productCategory).
		Str("issue", x.Issue)

	if info.capAlert == nil {
		event.Msg("Received weather product")
		return
	}
	capInfo := info.capAlert.GetPrimaryInfo()
	if capInfo == nil {
		event.Msg("Received CAP alert (no info block)")
		return
	}
	event.
		Str("cap_event", capInfo.Event).
		Str("cap_severity", capInfo.Severity).
		Str("cap_urgency", capInfo.Urgency).
		Str("cap_certainty", capInfo.Certainty)
	if len(capInfo.Area) > 0 {
		event.Str("cap_areas", capInfo.Area[0].AreaDesc)
	}
	if capInfo.Headline != "" {
		event.Str("cap_headline", capInfo.Headline)
	}
	event.Msg("Received CAP alert")
}

func buildDisplayName(info *productInfo) string {
	if info.capAlert != nil {
		if capInfo := info.capAlert.GetPrimaryInfo(); capInfo != nil && capInfo.Event != "" {
			return fmt.Sprintf("%s [%s/%s]", capInfo.Event, capInfo.Severity, capInfo.Urgency)
		}
	}
	if info.productCategory != "Unknown" {
		return fmt.Sprintf("%s (%s)", info.productName, info.productCategory)
	}
	return info.productName
}

func formatAlertMessage(x *nwwsio.NWWSOIMessageXExtension, info *productInfo) string {
	if info.capAlert != nil && info.capAlert.GetPrimaryInfo() != nil {
		return formatCAPAlert(x, info.capAlert)
	}
	return formatRegularProduct(x, info.productName)
}

func formatCAPAlert(x *nwwsio.NWWSOIMessageXExtension, capAlert *nwwsio.Alert) string {
	capInfo := capAlert.GetPrimaryInfo()

	msg := fmt.Sprintf(
		"[%s] %s\n"+
			"Severity: %s | Urgency: %s | Certainty: %s\n"+
			"Product: %s | Issued: %s\n",
		x.Cccc, capInfo.Event, capInfo.Severity, capInfo.Urgency, capInfo.Certainty, x.AwipsID, x.Issue,
	)
	if capInfo.Headline != "" {
		msg += fmt.Sprintf("\n%s\n", capInfo.Headline)
	}
	if len(capInfo.Area) > 0 && capInfo.Area[0].AreaDesc != "" {
		msg += fmt.Sprintf("\nAffected Areas: %s\n", capInfo.Area[0].AreaDesc)
	}
	if capInfo.Description != "" {
		msg += "\n" + truncateText(capInfo.Description, MaxCAPDescriptionLen)
	} else {
		msg += "\n" + truncateText(x.Text, MaxCAPDescriptionLen)
	}
	if capInfo.Instruction != "" {
		msg += "\n\nInstructions: " + truncateText(capInfo.Instruction, MaxCAPInstructionLen)
	}
	return msg
}

func formatRegularProduct(x *nwwsio.NWWSOIMessageXExtension, productName string) string {
	return fmt.Sprintf(
		"[%s] %s\n"+
			"Product: %s | Issued: %s\n\n"+
			"%s",
		x.Cccc, productName, x.AwipsID, x.Issue, truncateText(x.Text, MaxRegularProductLen),
	)
}

func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "...\n[Message truncated]"
}
