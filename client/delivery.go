package client

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"
	nwwsio "github.com/seabird-chat/seabird-nwwsio-plugin/internal"
)

// shouldSendToSubscriber applies a subscription's filters: "all" takes every
// product, "cap" takes CAP alerts, and a category name takes the text
// products in that category.
func shouldSendToSubscriber(sub Subscription, productCategory string, isCAP bool) bool {
	for _, filter := range sub.Filters {
		switch strings.ToLower(filter) {
		case "all":
			return true
		case "cap":
			if isCAP {
				return true
			}
		case strings.ToLower(productCategory):
			return true
		}
	}
	return false
}

func deliverToStationSubscribers(client *SeabirdClient, x *nwwsio.NWWSOIMessageXExtension, info *productInfo, alertMsg string) {
	isCAP := info.capAlert != nil
	for _, sub := range client.subscriptions.GetStationSubscriptions(x.Cccc) {
		if !shouldSendToSubscriber(sub, info.productCategory, isCAP) {
			continue
		}
		client.SendPrivateMessage(sub.UserID, alertMsg)
		log.Info().
			Str("user_id", sub.UserID).
			Str("station", x.Cccc).
			Strs("filters", sub.Filters).
			Str("product_category", info.productCategory).
			Bool("is_cap", isCAP).
			Msg("Sent weather alert to subscriber")
	}
}

// productSAMECodes returns the SAME codes a product covers: CAP alerts carry
// them as geocodes, text products get them from their UGC block.
func productSAMECodes(info *productInfo, text string) []string {
	if info.capAlert != nil {
		return info.capAlert.AllSAMECodes()
	}
	return nwwsio.SAMECodesForProduct(text)
}

func deliverToGeoSubscribers(client *SeabirdClient, x *nwwsio.NWWSOIMessageXExtension, info *productInfo, alertMsg string, codes []string) {
	if len(codes) == 0 {
		return
	}
	isCAP := info.capAlert != nil
	for _, r := range geoRecipients(client.subscriptions, codes, info.productCategory, isCAP) {
		client.SendPrivateMessage(r.UserID, alertMsg+formatMatchedTrailer(r.Matched))
		log.Info().
			Str("user_id", r.UserID).
			Str("station", x.Cccc).
			Strs("matched", r.Matched).
			Str("product_category", info.productCategory).
			Bool("is_cap", isCAP).
			Msg("Sent weather alert to geographic subscriber")
	}
}

type geoRecipient struct {
	UserID  string
	Matched []string
}

// geoRecipients finds every user whose SAME or ZIP subscription covers one of
// the product's SAME codes and whose filters accept the product. Each user
// appears once per product with everything that matched, sorted by user ID.
func geoRecipients(sm *SubscriptionManager, codes []string, productCategory string, isCAP bool) []geoRecipient {
	matched := make(map[string][]string)
	add := func(sub Subscription, desc string) {
		if shouldSendToSubscriber(sub, productCategory, isCAP) {
			matched[sub.UserID] = append(matched[sub.UserID], desc)
		}
	}

	for _, code := range codes {
		for _, sub := range sm.GetSAMESubscriptions(code) {
			add(sub, describeSAMECode(code))
		}
	}

	codeSet := make(map[string]bool, len(codes))
	for _, code := range codes {
		codeSet[code] = true
	}
	zipSubs := sm.GetAllZIPSubscriptions()
	zips := make([]string, 0, len(zipSubs))
	for zip := range zipSubs {
		zips = append(zips, zip)
	}
	sort.Strings(zips)
	for _, zip := range zips {
		var hits []string
		for _, code := range nwwsio.ZIPToSAME(zip) {
			if !codeSet[code] {
				continue
			}
			if name, ok := nwwsio.SAMECountyName(code); ok {
				hits = append(hits, code+" "+name)
			} else {
				hits = append(hits, code)
			}
		}
		if len(hits) == 0 {
			continue
		}
		desc := fmt.Sprintf("ZIP %s (%s)", zip, strings.Join(hits, "; "))
		for _, sub := range zipSubs[zip] {
			add(sub, desc)
		}
	}

	recipients := make([]geoRecipient, 0, len(matched))
	for userID, descs := range matched {
		recipients = append(recipients, geoRecipient{UserID: userID, Matched: descs})
	}
	sort.Slice(recipients, func(i, j int) bool { return recipients[i].UserID < recipients[j].UserID })
	return recipients
}

func formatMatchedTrailer(matched []string) string {
	return "\n\nMatched: " + strings.Join(matched, "; ")
}
