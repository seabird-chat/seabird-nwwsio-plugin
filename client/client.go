package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"
	nwwsio "github.com/seabird-chat/seabird-nwwsio-plugin/internal"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gosrc.io/xmpp"
	"gosrc.io/xmpp/stanza"
)

const (
	// NWWS Servers, see https://www.weather.gov/nwws/#access
	NWWSBoulder     string = "nwws-oi-bldr.weather.gov"
	NWWSCollegePark string = "nwws-oi-cprk.weather.gov"
	NWWSServerPort  string = "5222"
	NWWSDomain      string = "nwws-oi.weather.gov"
	NWWSResource    string = "nwws"

	// Message limits
	MaxRecentMessages    = 5
	MaxCAPDescriptionLen = 800
	MaxCAPInstructionLen = 200
	MaxRegularProductLen = 1000
	MUCReconnectDelay    = 5 * time.Second
	ConnectionTimeout    = 3 * time.Second
)

var Version = "v0.0.0-dev"

// generateInstanceID creates a short unique identifier for this instance
func generateInstanceID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if random fails
		return fmt.Sprintf("%d", time.Now().Unix()%10000)
	}
	return hex.EncodeToString(b)
}

// SeabirdClient is a basic client for seabird
type SeabirdClient struct {
	*seabird.Client
	NWWSClient     *xmpp.StreamManager
	nwwsXMPPClient *xmpp.Client
	mucJID         *stanza.Jid
	subscriptions  *SubscriptionManager

	// Sequence tracking for detecting missed messages
	sequenceMu   sync.Mutex
	lastSequence map[string]int // maps processID -> last sequence number

	// Context for graceful shutdown
	ctx        context.Context
	cancelFunc context.CancelFunc
}

// NewSeabirdClient returns a new seabird client
func NewSeabirdClient(seabirdCoreURL, seabirdCoreToken, nwwsioUsername, nwwsioPassword string) (*SeabirdClient, error) {
	log.Info().Str("url", seabirdCoreURL).Msg("Connecting to seabird-core")
	seabirdClient, err := seabird.NewClient(seabirdCoreURL, seabirdCoreToken)
	if err != nil {
		return nil, err
	}
	log.Info().Str("url", seabirdCoreURL).Msg("Successfully connected to seabird-core")

	instanceID := generateInstanceID()
	mucJID := &stanza.Jid{
		Node:     "nwws",
		Domain:   "conference.nwws-oi.weather.gov",
		Resource: fmt.Sprintf("%s-%s", nwwsioUsername, instanceID),
	}
	log.Info().Str("instance_id", instanceID).Msg("Generated unique instance ID")

	client := &SeabirdClient{
		Client:        seabirdClient,
		mucJID:        mucJID,
		subscriptions: NewSubscriptionManager(),
		lastSequence:  make(map[string]int),
	}

	log.Info().Str("username", nwwsioUsername).Msg("Connecting to NWWS-IO")
	nwwsioClient, nwwsXMPPClient, err := NewNWWSIOClient(nwwsioUsername, nwwsioPassword, instanceID, mucJID, client)
	if err != nil {
		return nil, err
	}
	log.Info().Str("username", nwwsioUsername).Msg("Successfully connected to NWWS-IO")

	client.NWWSClient = nwwsioClient
	client.nwwsXMPPClient = nwwsXMPPClient

	return client, nil
}

func (c *SeabirdClient) Shutdown() error {
	log.Info().Msg("Shutting down gracefully")

	// Cancel the context to signal all goroutines to stop
	if c.cancelFunc != nil {
		c.cancelFunc()
	}

	if c.nwwsXMPPClient != nil && c.mucJID != nil {
		err := c.nwwsXMPPClient.Send(stanza.Presence{
			Attrs: stanza.Attrs{
				To:   c.mucJID.Full(),
				Type: stanza.PresenceTypeUnavailable,
			},
		})
		if err != nil {
			log.Error().Err(err).Msg("Failed to send presence unavailable")
		}
	}

	if c.NWWSClient != nil {
		c.NWWSClient.Stop()
	}

	if c.Client != nil {
		return c.Client.Close()
	}
	return nil
}

// getAvailableNWWSIOSite attempts to connect to college park & boulder NWWS-IO
// sites and will return an XMPP client for the first successful site.
func getAvailableNWWSIOSite(nwwsioUsername, nwwsioPassword, instanceID string) (onlineNWWSIOConfig *xmpp.Config, err error) {
	router := xmpp.NewRouter()
	config := xmpp.Config{
		Jid:            fmt.Sprintf("%s@%s/%s-%s", nwwsioUsername, NWWSDomain, NWWSResource, instanceID),
		Credential:     xmpp.Password(nwwsioPassword),
		Insecure:       false,
		ConnectTimeout: int(ConnectionTimeout.Seconds()),
	}

	collegeParkConfig := xmpp.TransportConfiguration{
		Address: fmt.Sprintf("%s:%s", NWWSCollegePark, NWWSServerPort),
		Domain:  NWWSDomain,
	}
	config.TransportConfiguration = collegeParkConfig

	client, err := xmpp.NewClient(&config, router, errorHandler)
	if err != nil {
		return nil, err
	}
	log.Info().Str("site", config.Address).Msg("Testing connection to NWWS-IO site")
	err = client.Connect()
	if err != nil {
		log.Warn().Err(err).Str("failed_site", NWWSCollegePark).Str("trying_site", NWWSBoulder).Msg("Failed to connect to NWWS-IO server, trying backup")
		_ = client.Disconnect()

		boulderConfig := xmpp.TransportConfiguration{
			Address: fmt.Sprintf("%s:%s", NWWSBoulder, NWWSServerPort),
			Domain:  NWWSDomain,
		}
		config.TransportConfiguration = boulderConfig

		client, err = xmpp.NewClient(&config, router, errorHandler)
		if err != nil {
			return nil, err
		}

		log.Info().Str("site", config.Address).Msg("Testing connection to NWWS-IO site")
		err = client.Connect()
		if err != nil {
			_ = client.Disconnect()
			log.Error().Msg("Failed to connect to all NWWS-IO sites")
			return nil, fmt.Errorf("Failed to connect to all NWWS-IO sites")
		}
	}
	err = client.Disconnect()
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// NewNWWSIOClient returns a new NWWS-IO Client
func NewNWWSIOClient(nwwsioUsername, nwwsioPassword, instanceID string, mucJID *stanza.Jid, client *SeabirdClient) (*xmpp.StreamManager, *xmpp.Client, error) {
	onlineClientConfig, err := getAvailableNWWSIOSite(nwwsioUsername, nwwsioPassword, instanceID)
	if err != nil {
		return nil, nil, err
	}

	router := xmpp.NewRouter()
	router.HandleFunc("message", func(s xmpp.Sender, p stanza.Packet) {
		handleMessage(s, p, client)
	})
	router.HandleFunc("presence", func(s xmpp.Sender, p stanza.Packet) {
		handlePresence(s, p, mucJID)
	})
	router.NewRoute().IQNamespaces("jabber:iq:version").HandlerFunc(handleVersion)

	onlineClient, err := xmpp.NewClient(onlineClientConfig, router, func(err error) {
		mucErrorHandler(err, mucJID)
	})
	if err != nil {
		return nil, nil, err
	}

	cm := xmpp.NewStreamManager(onlineClient, nwwsioPostConnect(mucJID))
	return cm, onlineClient, nil
}

func nwwsioPostConnect(mucJID *stanza.Jid) func(xmpp.Sender) {
	return func(c xmpp.Sender) {
		log.Info().Msg("NWWS-IO connection established")
		err := joinMUC(c, mucJID)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to join Multi-user Chat")
		}
		log.Info().Str("jid", mucJID.Full()).Msg("Successfully joined Multi-user Chat - ready to receive messages")
	}
}

func joinMUC(c xmpp.Sender, toJID *stanza.Jid) error {
	log.Info().Str("jid", toJID.Full()).Msg("Attempting to join Multi-user chat")
	return c.Send(stanza.Presence{Attrs: stanza.Attrs{To: toJID.Full()},
		Extensions: []stanza.PresExtension{
			stanza.MucPresence{
				History: stanza.History{MaxStanzas: stanza.NewNullableInt(0)},
			}},
	})
}

// checkSequenceGaps detects and logs any missed messages based on sequence IDs
func checkSequenceGaps(client *SeabirdClient, processID string, sequenceID int) {
	client.sequenceMu.Lock()
	defer client.sequenceMu.Unlock()

	lastSeq, exists := client.lastSequence[processID]
	if exists {
		expected := lastSeq + 1
		if sequenceID != expected {
			missedCount := sequenceID - expected
			log.Warn().
				Str("process_id", processID).
				Int("expected_seq", expected).
				Int("received_seq", sequenceID).
				Int("missed_count", missedCount).
				Msg("Detected missed messages - sequence gap")
		}
	}
	client.lastSequence[processID] = sequenceID
}

// productInfo holds parsed product identification information
type productInfo struct {
	productID       *nwwsio.WMOProductID
	productName     string
	productCategory string
	capAlert        *nwwsio.Alert
}

// parseProductInfo extracts product identification from the NWWS message
func parseProductInfo(messageNWWSIOX *nwwsio.NWWSOIMessageXExtension) (*productInfo, error) {
	productID, err := messageNWWSIOX.ParseTtaaii()
	if err != nil {
		return nil, fmt.Errorf("failed to parse WMO product ID: %w", err)
	}

	// Default to WMO data type
	info := &productInfo{
		productID:       productID,
		productName:     productID.GetDataType(),
		productCategory: "Unknown",
	}

	// Try to get more specific product info from AWIPS ID
	awipsID, err := messageNWWSIOX.ParseAwipsID()
	if err != nil {
		log.Debug().
			Err(err).
			Str("awipsid", messageNWWSIOX.AwipsID).
			Str("cccc", messageNWWSIOX.Cccc).
			Str("ttaaii", messageNWWSIOX.Ttaaii).
			Msg("Failed to parse AWIPS ID, using WMO type as fallback")
	} else {
		info.productName = awipsID.GetProductName()
		info.productCategory = awipsID.GetProductCategory()
	}

	// Try to parse CAP message if it looks like one
	if isLikelyCAP(productID, messageNWWSIOX.Text) {
		capAlert, err := nwwsio.ParseCAP(messageNWWSIOX.Text)
		if err != nil {
			log.Debug().Err(err).Msg("Failed to parse CAP message")
		} else {
			info.capAlert = capAlert
		}
	}

	return info, nil
}

// logProductReceipt logs the received weather product with appropriate detail
func logProductReceipt(messageNWWSIOX *nwwsio.NWWSOIMessageXExtension, info *productInfo) {
	baseLog := log.Info().
		Str("cccc", messageNWWSIOX.Cccc).
		Str("ttaaii", messageNWWSIOX.Ttaaii).
		Str("wmo_type", info.productID.GetDataType()).
		Str("awipsid", messageNWWSIOX.AwipsID).
		Str("product", info.productName).
		Str("category", info.productCategory).
		Str("issue", messageNWWSIOX.Issue)

	if info.capAlert != nil {
		capInfo := info.capAlert.GetPrimaryInfo()
		if capInfo != nil {
			baseLog.
				Str("cap_event", capInfo.Event).
				Str("cap_severity", capInfo.Severity).
				Str("cap_urgency", capInfo.Urgency).
				Str("cap_certainty", capInfo.Certainty)

			if len(capInfo.Area) > 0 {
				baseLog.Str("cap_areas", capInfo.Area[0].AreaDesc)
			}
			if capInfo.Headline != "" {
				baseLog.Str("cap_headline", capInfo.Headline)
			}
			baseLog.Msg("Received CAP alert")
		} else {
			baseLog.Msg("Received CAP alert (no info block)")
		}
	} else {
		baseLog.Msg("Received weather product")
	}
}

// buildDisplayName creates a human-readable display name for the product
func buildDisplayName(info *productInfo) string {
	displayName := info.productName
	if info.productCategory != "Unknown" {
		displayName = fmt.Sprintf("%s (%s)", info.productName, info.productCategory)
	}

	// Enhance with CAP alert details if available
	if info.capAlert != nil {
		capInfo := info.capAlert.GetPrimaryInfo()
		if capInfo != nil && capInfo.Event != "" {
			displayName = fmt.Sprintf("%s [%s/%s]", capInfo.Event, capInfo.Severity, capInfo.Urgency)
		}
	}

	return displayName
}

// formatAlertMessage formats the alert message for delivery to subscribers
func formatAlertMessage(messageNWWSIOX *nwwsio.NWWSOIMessageXExtension, info *productInfo) string {
	if info.capAlert != nil && info.capAlert.GetPrimaryInfo() != nil {
		return formatCAPAlert(messageNWWSIOX, info.capAlert)
	}
	return formatRegularProduct(messageNWWSIOX, info.productName)
}

// formatCAPAlert formats a CAP alert message with full details
func formatCAPAlert(messageNWWSIOX *nwwsio.NWWSOIMessageXExtension, capAlert *nwwsio.Alert) string {
	capInfo := capAlert.GetPrimaryInfo()

	msg := fmt.Sprintf(
		"[%s] %s\n"+
			"Severity: %s | Urgency: %s | Certainty: %s\n"+
			"Product: %s | Issued: %s\n",
		messageNWWSIOX.Cccc,
		capInfo.Event,
		capInfo.Severity,
		capInfo.Urgency,
		capInfo.Certainty,
		messageNWWSIOX.AwipsID,
		messageNWWSIOX.Issue,
	)

	if capInfo.Headline != "" {
		msg += fmt.Sprintf("\n%s\n", capInfo.Headline)
	}

	if len(capInfo.Area) > 0 && capInfo.Area[0].AreaDesc != "" {
		msg += fmt.Sprintf("\nAffected Areas: %s\n", capInfo.Area[0].AreaDesc)
	}

	if capInfo.Description != "" {
		msg += fmt.Sprintf("\n%s", truncateText(capInfo.Description, MaxCAPDescriptionLen))
	} else {
		msg += fmt.Sprintf("\n%s", truncateText(messageNWWSIOX.Text, MaxCAPDescriptionLen))
	}

	if capInfo.Instruction != "" {
		msg += fmt.Sprintf("\n\nInstructions: %s", truncateText(capInfo.Instruction, MaxCAPInstructionLen))
	}

	return msg
}

// formatRegularProduct formats a non-CAP weather product message
func formatRegularProduct(messageNWWSIOX *nwwsio.NWWSOIMessageXExtension, productName string) string {
	return fmt.Sprintf(
		"[%s] %s\n"+
			"Product: %s | Issued: %s\n\n"+
			"%s",
		messageNWWSIOX.Cccc,
		productName,
		messageNWWSIOX.AwipsID,
		messageNWWSIOX.Issue,
		truncateText(messageNWWSIOX.Text, MaxRegularProductLen),
	)
}

// shouldSendToSubscriber determines if a subscriber should receive this message based on their filters
func shouldSendToSubscriber(sub Subscription, productCategory string, isCAP bool) bool {
	for _, filter := range sub.Filters {
		filterLower := strings.ToLower(filter)

		if filterLower == "all" {
			return true
		}
		if filterLower == "cap" && isCAP {
			return true
		}
		if filterLower == strings.ToLower(productCategory) {
			return true
		}
	}
	return false
}

// deliverToSubscribers sends the alert message to all matching subscribers
func deliverToSubscribers(client *SeabirdClient, messageNWWSIOX *nwwsio.NWWSOIMessageXExtension, info *productInfo, alertMsg string) {
	subscriptions := client.subscriptions.GetStationSubscriptions(messageNWWSIOX.Cccc)
	if len(subscriptions) == 0 {
		return
	}

	isCAP := info.capAlert != nil

	for _, sub := range subscriptions {
		if shouldSendToSubscriber(sub, info.productCategory, isCAP) {
			client.SendPrivateMessage(sub.UserID, alertMsg)
			log.Info().
				Str("user_id", sub.UserID).
				Str("station", messageNWWSIOX.Cccc).
				Strs("filters", sub.Filters).
				Str("product_category", info.productCategory).
				Bool("is_cap", isCAP).
				Msg("Sent weather alert to subscriber")
		}
	}
}

func handleMessage(s xmpp.Sender, p stanza.Packet, client *SeabirdClient) {
	// Only process Message packets
	msg, ok := p.(stanza.Message)
	if !ok {
		log.Debug().Str("type", fmt.Sprintf("%T", p)).Msg("Ignoring packet")
		return
	}

	// Extract NWWS-OI extension from message
	var messageNWWSIOX nwwsio.NWWSOIMessageXExtension
	if ok := msg.Get(&messageNWWSIOX); !ok {
		return
	}

	// Normalize AWIPS ID by trimming any whitespace from XML parsing
	messageNWWSIOX.AwipsID = strings.TrimSpace(messageNWWSIOX.AwipsID)

	// Check for sequence gaps in the message stream
	processID, sequenceID, err := messageNWWSIOX.GetSequenceID()
	if err != nil {
		log.Debug().Err(err).Str("id", messageNWWSIOX.ID).Msg("Failed to parse sequence ID")
	} else {
		checkSequenceGaps(client, processID, sequenceID)
	}

	// Parse product identification information
	info, err := parseProductInfo(&messageNWWSIOX)
	if err != nil {
		log.Warn().Err(err).Str("ttaaii", messageNWWSIOX.Ttaaii).Msg("Failed to parse product info")
		return
	}

	// Log receipt of this weather product
	logProductReceipt(&messageNWWSIOX, info)

	// Build a user-friendly display name for the product
	displayName := buildDisplayName(info)

	// Store this message in recent history for the station
	client.subscriptions.AddRecentMessage(RecentMessage{
		Station:   messageNWWSIOX.Cccc,
		DataType:  displayName,
		AwipsID:   messageNWWSIOX.AwipsID,
		Issue:     messageNWWSIOX.Issue,
		Text:      messageNWWSIOX.Text,
		Timestamp: time.Now(),
	})

	// Deliver to any subscribers for this station
	alertMsg := formatAlertMessage(&messageNWWSIOX, info)
	deliverToSubscribers(client, &messageNWWSIOX, info, alertMsg)
}

func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "...\n[Message truncated]"
}

func isLikelyCAP(productID *nwwsio.WMOProductID, text string) bool {
	return productID.T1 == "X" || strings.Contains(text, "<alert")
}

func errorHandler(err error) {
	log.Error().Err(err).Msg("XMPP error")
}

// mucErrorHandler provides enhanced error handling with MUC recovery
func mucErrorHandler(err error, mucJID *stanza.Jid) {
	errMsg := err.Error()

	// Check for MUC namespace errors
	if strings.Contains(errMsg, "unknown namespace") && strings.Contains(errMsg, "jabber.org/protocol/muc") {
		log.Warn().
			Err(err).
			Str("muc_jid", mucJID.Full()).
			Msg("MUC namespace parsing error detected - this is a known issue with certain presence stanzas, ignoring")
		// Don't attempt rejoin for namespace errors - they're usually non-fatal parsing issues
		return
	}

	// For other errors, log them normally
	log.Error().Err(err).Msg("XMPP error")
}

// handlePresence handles XMPP presence stanzas, particularly for MUC
func handlePresence(s xmpp.Sender, p stanza.Packet, mucJID *stanza.Jid) {
	presence, ok := p.(*stanza.Presence)
	if !ok {
		return
	}

	log.Debug().
		Str("from", presence.From).
		Str("to", presence.To).
		Str("type", string(presence.Type)).
		Msg("Received presence stanza")

	// Check for error presences from the MUC
	if presence.Type == stanza.PresenceTypeError && strings.HasPrefix(presence.From, mucJID.Bare()) {
		log.Warn().
			Str("from", presence.From).
			Str("error_type", string(presence.Type)).
			Msg("Received error presence from MUC")

		// Attempt to rejoin the MUC after a short delay
		go func() {
			time.Sleep(MUCReconnectDelay)
			log.Info().Str("muc_jid", mucJID.Full()).Msg("Attempting to rejoin MUC after error")
			if err := joinMUC(s, mucJID); err != nil {
				log.Error().Err(err).Msg("Failed to rejoin MUC")
			} else {
				log.Info().Msg("Successfully rejoined MUC")
			}
		}()
	}
}

func handleVersion(c xmpp.Sender, p stanza.Packet) {
	// Type conversion & sanity checks
	iq, ok := p.(*stanza.IQ)
	if !ok {
		return
	}

	iqResp, err := stanza.NewIQ(stanza.Attrs{Type: "result", From: iq.To, To: iq.From, Id: iq.Id, Lang: "en"})
	if err != nil {
		return
	}

	iqResp.Version().SetInfo("seabird-nwwsio-plugin", Version, fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH))
	_ = c.Send(iqResp)
}

func (c *SeabirdClient) SendMessage(channelID, text string) {
	ctx := context.Background()
	_, err := c.Client.Inner.SendMessage(ctx, &pb.SendMessageRequest{
		ChannelId: channelID,
		Text:      text,
	})
	if err != nil {
		log.Error().Err(err).Str("channel_id", channelID).Msg("Failed to send message")
	} else {
		log.Debug().Str("channel_id", channelID).Int("length", len(text)).Msg("Sent message to channel")
	}
}

func (c *SeabirdClient) SendPrivateMessage(userID, text string) {
	ctx := context.Background()
	_, err := c.Client.Inner.SendPrivateMessage(ctx, &pb.SendPrivateMessageRequest{
		UserId: userID,
		Text:   text,
	})
	if err != nil {
		log.Error().Err(err).Str("user_id", userID).Msg("Failed to send private message")
	}
}

func (c *SeabirdClient) handleCommandEvents(ctx context.Context) {
	commands := map[string]*pb.CommandMetadata{
		"noaa": {
			Name:      "noaa",
			ShortHelp: "Subscribe to NOAA weather alerts",
			FullHelp:  "Usage: !noaa <help|subscribe|unsubscribe|list|recent> [options]. Use !noaa help for details.",
		},
	}

	log.Info().Int("command_count", len(commands)).Msg("Attempting to register commands with seabird-core")

	stream, err := c.Client.StreamEvents(commands)
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.AlreadyExists {
			log.Fatal().Err(err).Msg("Another instance of this plugin is already running. Please stop the other instance first.")
		}
		log.Error().Err(err).Msg("Failed to stream events")
		return
	}
	defer func() {
		log.Info().Msg("Closing event stream")
		if err := stream.Close(); err != nil {
			log.Error().Err(err).Msg("Error closing stream")
		}
	}()

	log.Info().Msg("Event stream established - ready to receive commands")

	eventCount := 0
	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Context cancelled - stopping command handler")
			return
		case event, ok := <-stream.C:
			if !ok {
				log.Warn().Int("total_events", eventCount).Msg("Event stream channel closed - exiting command handler")
				return
			}
			eventCount++
			log.Info().Int("event_count", eventCount).Msg("Received event from stream")
			if cmd := event.GetCommand(); cmd != nil {
				c.handleNoaaCommand(event, cmd)
			}
		}
	}
}

func buildFilterConfirmation(stationCode string, filters []string) string {
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

	if hasAll {
		return fmt.Sprintf("You'll receive DMs for ALL weather products from %s.", stationCode)
	}
	if hasCAP && len(categories) == 0 {
		return fmt.Sprintf("You'll receive DMs for emergency alerts (CAP) from %s.", stationCode)
	}
	if len(categories) > 0 && !hasCAP {
		return fmt.Sprintf("You'll receive DMs for %s products from %s.", strings.Join(categories, ", "), stationCode)
	}
	return fmt.Sprintf("You'll receive DMs for CAP alerts and %s products from %s.", strings.Join(categories, ", "), stationCode)
}

func (c *SeabirdClient) handleNoaaCommand(event *pb.Event, cmd *pb.CommandEvent) {
	log.Info().
		Str("user_id", cmd.Source.User.Id).
		Str("user_name", cmd.Source.User.DisplayName).
		Str("channel_id", cmd.Source.ChannelId).
		Str("command", cmd.Command).
		Str("arg", cmd.Arg).
		Msg("Received !noaa command")

	args := strings.Fields(cmd.Arg)
	if len(args) < 1 {
		c.SendMessage(cmd.Source.ChannelId, "Usage: !noaa <subscribe|unsubscribe|list> <station|zip> [code]")
		return
	}

	action := strings.ToLower(args[0])

	switch action {
	case "help":
		helpMsg := "NOAA Weather Alerts: !noaa subscribe station <CODE> [filters...] | unsubscribe station <CODE> | unsubscribe all | list | recent <CODE> | filters | help. Example: !noaa subscribe station KJAX warning"
		c.SendMessage(cmd.Source.ChannelId, helpMsg)

	case "filters":
		validFilters := GetValidFilters()
		msg := "Valid filter options:\n"
		msg += "Special: all, cap\n"
		msg += "Categories: " + strings.Join(validFilters[2:], ", ")
		c.SendMessage(cmd.Source.ChannelId, msg)

	case "subscribe":
		if len(args) < 3 {
			c.SendMessage(cmd.Source.ChannelId, "Usage: !noaa subscribe station <code> [filters...]")
			c.SendMessage(cmd.Source.ChannelId, "Filters: cap (default), all, or any product category")
			c.SendMessage(cmd.Source.ChannelId, "Use '!noaa filters' to see all valid filter options")
			return
		}
		subType := strings.ToLower(args[1])
		code := args[2]

		var filters []string
		if len(args) >= 4 {
			for _, arg := range args[3:] {
				for _, part := range strings.Split(arg, ",") {
					if trimmed := strings.TrimSpace(part); trimmed != "" {
						filters = append(filters, trimmed)
					}
				}
			}
		}
		if len(filters) == 0 {
			filters = []string{"cap"}
		}

		// Validate filters before subscribing
		if invalidFilters := ValidateFilters(filters); len(invalidFilters) > 0 {
			c.SendMessage(cmd.Source.ChannelId, fmt.Sprintf("Invalid filter(s): %s", strings.Join(invalidFilters, ", ")))
			c.SendMessage(cmd.Source.ChannelId, "Use '!noaa filters' to see all valid filter options")
			return
		}

		if subType == "station" {
			if err := ValidateStationCode(code); err != nil {
				c.SendMessage(cmd.Source.ChannelId, fmt.Sprintf("Invalid station code: %s", err))
				return
			}

			c.subscriptions.SubscribeToStation(cmd.Source.User.Id, code, filters)
			c.SendMessage(cmd.Source.ChannelId, fmt.Sprintf("Subscribed to station %s with filters: %s", strings.ToUpper(code), strings.Join(filters, ", ")))

			confirmMsg := buildFilterConfirmation(strings.ToUpper(code), filters)
			recent := c.subscriptions.GetRecentMessages(code)
			if len(recent) > 0 {
				lastMsg := recent[len(recent)-1]
				confirmMsg += fmt.Sprintf("\nLast activity: %s (%s ago)",
					lastMsg.DataType,
					time.Since(lastMsg.Timestamp).Round(time.Second))
			}
			c.SendPrivateMessage(cmd.Source.User.Id, confirmMsg)

		} else {
			c.SendMessage(cmd.Source.ChannelId, "Invalid subscription type. Use 'station'")
		}

	case "unsubscribe":
		if len(args) < 2 {
			c.SendMessage(cmd.Source.ChannelId, "Usage: !noaa unsubscribe <station|all> [code]")
			return
		}
		subType := strings.ToLower(args[1])

		if subType == "all" {
			count := c.subscriptions.UnsubscribeFromAll(cmd.Source.User.Id)
			if count > 0 {
				c.SendMessage(cmd.Source.ChannelId, fmt.Sprintf("Removed %d subscription(s)", count))
			} else {
				c.SendMessage(cmd.Source.ChannelId, "You have no active subscriptions")
			}
			return
		}

		if len(args) < 3 {
			c.SendMessage(cmd.Source.ChannelId, "Usage: !noaa unsubscribe station <code>")
			return
		}
		code := args[2]

		if subType == "station" {
			if c.subscriptions.UnsubscribeFromStation(cmd.Source.User.Id, code) {
				c.SendMessage(cmd.Source.ChannelId, fmt.Sprintf("Unsubscribed from station %s", strings.ToUpper(code)))
			} else {
				c.SendMessage(cmd.Source.ChannelId, fmt.Sprintf("Not subscribed to station %s", strings.ToUpper(code)))
			}
		} else {
			c.SendMessage(cmd.Source.ChannelId, "Invalid subscription type. Use 'station' or 'all'")
		}

	case "recent":
		if len(args) < 2 {
			c.SendMessage(cmd.Source.ChannelId, "Usage: !noaa recent <station_code>")
			return
		}
		stationCode := strings.ToUpper(args[1])
		messages := c.subscriptions.GetRecentMessages(stationCode)

		if len(messages) == 0 {
			c.SendMessage(cmd.Source.ChannelId, fmt.Sprintf("No recent messages from %s", stationCode))
			return
		}

		var msg strings.Builder
		msg.WriteString(fmt.Sprintf("Recent messages from %s:\n", stationCode))
		for i, m := range messages {
			ago := time.Since(m.Timestamp).Round(time.Second)
			msg.WriteString(fmt.Sprintf("%d. %s - %s (%s ago)\n", i+1, m.DataType, m.AwipsID, ago))
		}
		c.SendMessage(cmd.Source.ChannelId, msg.String())

	case "list":
		stations := c.subscriptions.GetUserStationSubscriptions(cmd.Source.User.Id)

		msg := "Your subscriptions:\n"
		if len(stations) > 0 {
			msg += fmt.Sprintf("Stations: %s\n", strings.Join(stations, ", "))
		} else {
			msg = "You have no active subscriptions"
		}

		c.SendMessage(cmd.Source.ChannelId, msg)

	default:
		c.SendMessage(cmd.Source.ChannelId, "Unknown action. Use: subscribe, unsubscribe, or list")
	}
}

// Run runs both the NWWS client and seabird command handler concurrently
func (c *SeabirdClient) Run() error {
	// Create a cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	c.ctx = ctx
	c.cancelFunc = cancel
	defer cancel()

	g, gctx := errgroup.WithContext(ctx)

	log.Info().Msg("Starting NWWS-IO client")
	g.Go(func() error {
		return c.NWWSClient.Run()
	})

	log.Info().Msg("Starting seabird command handler")
	g.Go(func() error {
		c.handleCommandEvents(gctx)
		return nil
	})

	return g.Wait()
}
