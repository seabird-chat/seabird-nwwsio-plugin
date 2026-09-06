package client

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/seabird-chat/seabird-go/pb"
	nwwsio "github.com/seabird-chat/seabird-nwwsio-plugin/internal"
)

const (
	usageText = "Usage: !noaa <subscribe|unsubscribe|list|recent|filters|help>. Use '!noaa help' for details."

	helpText = "NOAA Weather Alerts commands:\n" +
		"!noaa subscribe station <CODE> [filters...]  e.g. !noaa subscribe station KJAX warning,watch\n" +
		"!noaa subscribe same <CODE> [<CODE>...] [filters...]  e.g. !noaa subscribe same 012031 012109\n" +
		"!noaa subscribe zip <ZIP> [<ZIP>...] [filters...]  e.g. !noaa subscribe zip 48103-1680 all\n" +
		"!noaa unsubscribe station <CODE> | same <CODE...>|all | zip <ZIP...>|all | all\n" +
		"!noaa list | recent <CODE> | filters | help\n" +
		"Filters default to cap (emergency alerts only). Use '!noaa filters' for the full list."

	subscribeUsage = "Usage: !noaa subscribe station <CODE> [filters...] | same <CODE> [<CODE>...] [filters...] | zip <ZIP> [<ZIP>...] [filters...]\n" +
		"Filters default to cap. Use '!noaa filters' to see all valid filter options"

	unsubscribeUsage = "Usage: !noaa unsubscribe station <CODE> | same <CODE...>|all | zip <ZIP...>|all | all"
)

// handleCommandEvents serves !noaa commands until ctx ends, reconnecting the
// seabird event stream whenever it drops.
func (c *SeabirdClient) handleCommandEvents(ctx context.Context) error {
	commands := map[string]*pb.CommandMetadata{
		"noaa": {
			Name:      "noaa",
			ShortHelp: "Subscribe to NOAA weather alerts",
			FullHelp:  "Usage: !noaa <subscribe|unsubscribe|list|recent|filters|help>. Subscribe by station, SAME code, or ZIP. Use !noaa help for details.",
		},
	}

	cs := &commandStream{
		open: func() (eventSource, error) {
			log.Info().Msg("Registering commands with seabird-core")
			stream, err := c.Client.StreamEvents(commands)
			if err != nil {
				return nil, err
			}
			return seabirdEventSource{stream}, nil
		},
		handle: func(event *pb.Event) {
			if cmd := event.GetCommand(); cmd != nil {
				c.handleNoaaCommand(cmd)
			}
		},
		wait: waitFor,
	}
	return cs.run(ctx)
}

func (c *SeabirdClient) handleNoaaCommand(cmd *pb.CommandEvent) {
	log.Info().
		Str("user_id", cmd.Source.User.Id).
		Str("user_name", cmd.Source.User.DisplayName).
		Str("channel_id", cmd.Source.ChannelId).
		Str("arg", cmd.Arg).
		Msg("Received !noaa command")

	channel := cmd.Source.ChannelId
	args := strings.Fields(cmd.Arg)
	if len(args) < 1 {
		c.SendMessage(channel, usageText)
		return
	}

	switch strings.ToLower(args[0]) {
	case "help":
		c.SendMessage(channel, helpText)

	case "filters":
		filters := GetValidFilters()
		c.SendMessage(channel, "Valid filter options:\nSpecial: all, cap\nCategories: "+strings.Join(filters[2:], ", "))

	case "subscribe":
		c.handleSubscribe(cmd, args[1:])

	case "unsubscribe":
		c.handleUnsubscribe(cmd, args[1:])

	case "recent":
		if len(args) < 2 {
			c.SendMessage(channel, "Usage: !noaa recent <station_code>")
			return
		}
		c.SendMessage(channel, formatRecentMessages(strings.ToUpper(args[1]), c.subscriptions.GetRecentMessages(args[1])))

	case "list":
		userID := cmd.Source.User.Id
		c.SendMessage(channel, formatSubscriptionList(
			c.subscriptions.GetUserStations(userID),
			c.subscriptions.GetUserSAMESubscriptions(userID),
			c.subscriptions.GetUserZIPSubscriptions(userID),
		))

	default:
		c.SendMessage(channel, "Unknown action. Use: subscribe, unsubscribe, list, recent, filters, or help")
	}
}

func (c *SeabirdClient) handleSubscribe(cmd *pb.CommandEvent, args []string) {
	channel, userID := cmd.Source.ChannelId, cmd.Source.User.Id
	if len(args) < 2 {
		c.SendMessage(channel, subscribeUsage)
		return
	}

	rejectFilters := func(filters []string) bool {
		invalid := ValidateFilters(filters)
		if len(invalid) == 0 {
			return false
		}
		c.SendMessage(channel, fmt.Sprintf("Invalid filter(s): %s. Use '!noaa filters' to see all valid filter options", strings.Join(invalid, ", ")))
		return true
	}

	kind := strings.ToLower(args[0])
	switch kind {
	case "station":
		code := strings.ToUpper(args[1])
		if err := ValidateStationCode(code); err != nil {
			c.SendMessage(channel, fmt.Sprintf("Invalid station code: %s", err))
			return
		}
		filters := splitArgs(args[2:])
		if rejectFilters(filters) {
			return
		}
		filters = normalizeFilters(filters)

		c.subscriptions.SubscribeToStation(userID, code, filters)
		c.SendMessage(channel, fmt.Sprintf("Subscribed to station %s with filters: %s", code, strings.Join(filters, ", ")))

		confirm := buildFilterConfirmation("from station "+code, filters)
		if recent := c.subscriptions.GetRecentMessages(code); len(recent) > 0 {
			last := recent[len(recent)-1]
			confirm += fmt.Sprintf("\nLast activity: %s (%s ago)", last.DataType, time.Since(last.Timestamp).Round(time.Second))
		}
		c.SendPrivateMessage(userID, confirm)

	case string(geoKindSAME), string(geoKindZIP):
		geo := geoKind(kind)
		codes, filters, errs := parseGeoSubscribeArgs(geo, args[1:])
		if len(errs) > 0 {
			c.SendMessage(channel, strings.Join(errs, "\n"))
			return
		}
		if rejectFilters(filters) {
			return
		}
		filters = normalizeFilters(filters)

		if geo == geoKindSAME {
			c.subscriptions.SubscribeToSAME(userID, codes, filters)
		} else {
			c.subscriptions.SubscribeToZIP(userID, codes, filters)
		}
		c.SendMessage(channel, buildGeoConfirmation(geo, codes, filters))
		c.SendPrivateMessage(userID, buildFilterConfirmation(fmt.Sprintf("covering %s %s", geo.label(), strings.Join(codes, ", ")), filters))

	default:
		c.SendMessage(channel, "Invalid subscription type. Use 'station', 'same' or 'zip'")
	}
}

func (c *SeabirdClient) handleUnsubscribe(cmd *pb.CommandEvent, args []string) {
	channel, userID := cmd.Source.ChannelId, cmd.Source.User.Id
	if len(args) < 1 {
		c.SendMessage(channel, unsubscribeUsage)
		return
	}

	kind := strings.ToLower(args[0])
	switch kind {
	case "all":
		if count := c.subscriptions.UnsubscribeFromAll(userID); count > 0 {
			c.SendMessage(channel, fmt.Sprintf("Removed %d subscription(s)", count))
		} else {
			c.SendMessage(channel, "You have no active subscriptions")
		}

	case "station":
		if len(args) < 2 {
			c.SendMessage(channel, "Usage: !noaa unsubscribe station <code>")
			return
		}
		code := strings.ToUpper(args[1])
		if c.subscriptions.UnsubscribeFromStation(userID, code) {
			c.SendMessage(channel, fmt.Sprintf("Unsubscribed from station %s", code))
		} else {
			c.SendMessage(channel, fmt.Sprintf("Not subscribed to station %s", code))
		}

	case string(geoKindSAME), string(geoKindZIP):
		geo := geoKind(kind)
		if len(args) < 2 {
			c.SendMessage(channel, unsubscribeUsage)
			return
		}
		if strings.ToLower(args[1]) == "all" {
			var count int
			if geo == geoKindSAME {
				count = c.subscriptions.UnsubscribeFromAllSAME(userID)
			} else {
				count = c.subscriptions.UnsubscribeFromAllZIP(userID)
			}
			if count > 0 {
				c.SendMessage(channel, fmt.Sprintf("Removed %d %s subscription(s)", count, geo.label()))
			} else {
				c.SendMessage(channel, fmt.Sprintf("You have no %s subscriptions", geo.label()))
			}
			return
		}

		var requested, removed []string
		for _, tok := range splitArgs(args[1:]) {
			if geo == geoKindZIP {
				if zip, err := nwwsio.NormalizeZIP(tok); err == nil {
					tok = zip
				}
			}
			requested = append(requested, tok)
		}
		if geo == geoKindSAME {
			removed = c.subscriptions.UnsubscribeFromSAME(userID, requested)
		} else {
			removed = c.subscriptions.UnsubscribeFromZIP(userID, requested)
		}

		if len(removed) == 0 {
			c.SendMessage(channel, fmt.Sprintf("Not subscribed to %s: %s", geo.label(), strings.Join(requested, ", ")))
			return
		}
		msg := fmt.Sprintf("Unsubscribed from %s: %s", geo.label(), strings.Join(removed, ", "))
		if missing := difference(requested, removed); len(missing) > 0 {
			msg += fmt.Sprintf(" (not subscribed: %s)", strings.Join(missing, ", "))
		}
		c.SendMessage(channel, msg)

	default:
		c.SendMessage(channel, "Invalid subscription type. Use 'station', 'same', 'zip' or 'all'")
	}
}

type geoKind string

const (
	geoKindSAME geoKind = "same"
	geoKindZIP  geoKind = "zip"
)

func (k geoKind) label() string {
	if k == geoKindZIP {
		return "ZIP codes"
	}
	return "SAME codes"
}

var codeLikeRe = regexp.MustCompile(`^\d+(-\d+)?$`)

// parseGeoSubscribeArgs splits a same/zip subscribe into codes and filters:
// numeric tokens are codes, validated for the kind; anything else is a filter.
func parseGeoSubscribeArgs(kind geoKind, args []string) (codes, filters, errs []string) {
	seen := make(map[string]bool)
	for _, tok := range splitArgs(args) {
		if !codeLikeRe.MatchString(tok) {
			filters = append(filters, tok)
			continue
		}
		code, err := normalizeGeoCode(kind, tok)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}
	if len(codes) == 0 && len(errs) == 0 {
		errs = append(errs, fmt.Sprintf("No %s given", kind.label()))
	}
	return codes, filters, errs
}

func normalizeGeoCode(kind geoKind, tok string) (string, error) {
	if kind == geoKindSAME {
		if err := nwwsio.ValidateSAMECode(tok); err != nil {
			return "", fmt.Errorf("Invalid SAME code %s: %v", tok, err)
		}
		return tok, nil
	}
	zip, err := nwwsio.NormalizeZIP(tok)
	if err != nil {
		return "", fmt.Errorf("Invalid ZIP code %s: %v", tok, err)
	}
	if len(nwwsio.ZIPToSAME(zip)) == 0 {
		return "", fmt.Errorf("Unknown ZIP code %s: no county mapping (PO-box-only ZIPs are not supported)", tok)
	}
	return zip, nil
}

// splitArgs splits whitespace-separated args that may also be comma-separated
func splitArgs(args []string) []string {
	var out []string
	for _, arg := range args {
		for _, part := range strings.Split(arg, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

func difference(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, s := range b {
		inB[s] = true
	}
	var out []string
	for _, s := range a {
		if !inB[s] {
			out = append(out, s)
		}
	}
	return out
}

func describeSAMECode(code string) string {
	if name, ok := nwwsio.SAMECountyName(code); ok {
		return fmt.Sprintf("%s (%s)", code, name)
	}
	return code
}

func describeZIP(zip string) string {
	codes := nwwsio.ZIPToSAME(zip)
	if len(codes) == 0 {
		return zip
	}
	names := make([]string, len(codes))
	for i, code := range codes {
		if name, ok := nwwsio.SAMECountyName(code); ok {
			names[i] = name
		} else {
			names[i] = code
		}
	}
	return fmt.Sprintf("%s (%s)", zip, strings.Join(names, "; "))
}

func describeGeoCode(kind geoKind, code string) string {
	if kind == geoKindZIP {
		return describeZIP(code)
	}
	if _, ok := nwwsio.SAMECountyName(code); !ok {
		return code + " (not a listed county; accepted)"
	}
	return describeSAMECode(code)
}

func buildGeoConfirmation(kind geoKind, codes, filters []string) string {
	parts := make([]string, len(codes))
	for i, code := range codes {
		parts[i] = describeGeoCode(kind, code)
	}
	return fmt.Sprintf("Subscribed to %s: %s with filters: %s", kind.label(), strings.Join(parts, ", "), strings.Join(filters, ", "))
}

// buildFilterConfirmation words the confirmation DM; target reads like
// "from station KJAX" or "covering SAME codes 012031, 012109".
func buildFilterConfirmation(target string, filters []string) string {
	var hasAll, hasCAP bool
	var categories []string
	for _, f := range filters {
		switch strings.ToLower(f) {
		case "all":
			hasAll = true
		case "cap":
			hasCAP = true
		default:
			categories = append(categories, f)
		}
	}

	switch {
	case hasAll:
		return fmt.Sprintf("You'll receive DMs for ALL weather products %s.", target)
	case hasCAP && len(categories) == 0:
		return fmt.Sprintf("You'll receive DMs for emergency alerts (CAP) %s.", target)
	case !hasCAP:
		return fmt.Sprintf("You'll receive DMs for %s products %s.", strings.Join(categories, ", "), target)
	default:
		return fmt.Sprintf("You'll receive DMs for CAP alerts and %s products %s.", strings.Join(categories, ", "), target)
	}
}

func formatSubscriptionList(stations, same, zips []UserSubscription) string {
	if len(stations) == 0 && len(same) == 0 && len(zips) == 0 {
		return "You have no active subscriptions"
	}

	line := func(label string, subs []UserSubscription, describe func(string) string) string {
		if len(subs) == 0 {
			return ""
		}
		parts := make([]string, len(subs))
		for i, s := range subs {
			parts[i] = fmt.Sprintf("%s [%s]", describe(s.Code), strings.Join(s.Filters, ", "))
		}
		return label + ": " + strings.Join(parts, ", ") + "\n"
	}

	msg := "Your subscriptions:\n" +
		line("Stations", stations, func(code string) string { return code }) +
		line("SAME codes", same, describeSAMECode) +
		line("ZIP codes", zips, describeZIP)
	return strings.TrimRight(msg, "\n")
}

func formatRecentMessages(station string, messages []RecentMessage) string {
	if len(messages) == 0 {
		return fmt.Sprintf("No recent messages from %s", station)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Recent messages from %s:\n", station)
	for i, m := range messages {
		fmt.Fprintf(&b, "%d. %s - %s (%s ago)\n", i+1, m.DataType, m.AwipsID, time.Since(m.Timestamp).Round(time.Second))
	}
	return b.String()
}
