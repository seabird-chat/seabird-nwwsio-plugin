package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"runtime"
	"strings"
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
		ConnectTimeout: 3,
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
	router.NewRoute().IQNamespaces("jabber:iq:version").HandlerFunc(handleVersion)

	onlineClient, err := xmpp.NewClient(onlineClientConfig, router, errorHandler)
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

func handleMessage(s xmpp.Sender, p stanza.Packet, client *SeabirdClient) {
	msg, ok := p.(stanza.Message)
	if !ok {
		log.Debug().Str("type", fmt.Sprintf("%T", p)).Msg("Ignoring packet")
		return
	}

	var messageNWWSIOX nwwsio.NWWSOIMessageXExtension
	if ok := msg.Get(&messageNWWSIOX); ok {
		productID, err := messageNWWSIOX.ParseTtaaii()
		if err != nil {
			log.Warn().Err(err).Str("ttaaii", messageNWWSIOX.Ttaaii).Msg("Failed to parse WMO product ID")
			return
		}

		awipsID, err := messageNWWSIOX.ParseAwipsID()
		if err != nil {
			log.Warn().Err(err).Str("awipsid", messageNWWSIOX.AwipsID).Msg("Failed to parse AWIPS ID")
			return
		}

		productName := awipsID.GetProductName()
		productCategory := awipsID.GetProductCategory()

		log.Info().
			Str("cccc", messageNWWSIOX.Cccc).
			Str("ttaaii", messageNWWSIOX.Ttaaii).
			Str("wmo_type", productID.GetDataType()).
			Str("awipsid", messageNWWSIOX.AwipsID).
			Str("product", productName).
			Str("category", productCategory).
			Str("issue", messageNWWSIOX.Issue).
			Msg("Received weather product")

		// Store both WMO data type and AWIPS product name for flexibility
		displayName := productName
		if productCategory != "Unknown" {
			displayName = fmt.Sprintf("%s (%s)", productName, productCategory)
		}

		client.subscriptions.AddRecentMessage(RecentMessage{
			Station:   messageNWWSIOX.Cccc,
			DataType:  displayName,
			AwipsID:   messageNWWSIOX.AwipsID,
			Issue:     messageNWWSIOX.Issue,
			Text:      messageNWWSIOX.Text,
			Timestamp: time.Now(),
		})

		subscribers := client.subscriptions.GetStationSubscribers(messageNWWSIOX.Cccc)
		if len(subscribers) > 0 {
			alertMsg := fmt.Sprintf(
				"[%s] %s\n"+
					"Product: %s | Issued: %s\n\n"+
					"%s",
				messageNWWSIOX.Cccc,
				productName,
				messageNWWSIOX.AwipsID,
				messageNWWSIOX.Issue,
				truncateText(messageNWWSIOX.Text, 1000),
			)

			for _, userID := range subscribers {
				client.SendPrivateMessage(userID, alertMsg)
				log.Info().
					Str("user_id", userID).
					Str("station", messageNWWSIOX.Cccc).
					Msg("Sent weather alert to subscriber")
			}
		}
	}
}

func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "...\n[Message truncated]"
}

func errorHandler(err error) {
	log.Error().Err(err).Msg("XMPP error")
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

func (c *SeabirdClient) handleCommandEvents() {
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
		log.Error().Err(err).Msg("Failed to stream events")
		return
	}
	defer func() {
		log.Info().Msg("Closing event stream")
		if err := stream.Close(); err != nil {
			// Check if error is AlreadyExists (another instance running)
			if st, ok := status.FromError(err); ok && st.Code() == codes.AlreadyExists {
				log.Fatal().Err(err).Msg("Another instance of this plugin is already running. Please stop the other instance first.")
			}
			log.Error().Err(err).Msg("Error closing stream")
		}
	}()

	log.Info().Msg("Event stream established - ready to receive commands")

	eventCount := 0
	for event := range stream.C {
		eventCount++
		log.Info().Int("event_count", eventCount).Msg("Received event from stream")
		if cmd := event.GetCommand(); cmd != nil {
			c.handleNoaaCommand(event, cmd)
		}
	}

	log.Warn().Int("total_events", eventCount).Msg("Event stream channel closed - exiting command handler")
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
		helpMsg := "NOAA Weather Alerts: !noaa subscribe station <CODE> | unsubscribe station <CODE> | unsubscribe all | list | recent <CODE> | help. Example: !noaa subscribe station KJAX"
		c.SendMessage(cmd.Source.ChannelId, helpMsg)

	case "subscribe":
		if len(args) < 3 {
			c.SendMessage(cmd.Source.ChannelId, "Usage: !noaa subscribe <station|zip> <code>")
			return
		}
		subType := strings.ToLower(args[1])
		code := args[2]

		if subType == "station" {
			if err := ValidateStationCode(code); err != nil {
				c.SendMessage(cmd.Source.ChannelId, fmt.Sprintf("Invalid station code: %s", err))
				return
			}

			c.subscriptions.SubscribeToStation(cmd.Source.User.Id, code)
			c.SendMessage(cmd.Source.ChannelId, fmt.Sprintf("Subscribed to station %s", strings.ToUpper(code)))

			recent := c.subscriptions.GetRecentMessages(code)
			confirmMsg := fmt.Sprintf("You'll receive DMs for all weather products from %s.", strings.ToUpper(code))
			if len(recent) > 0 {
				lastMsg := recent[len(recent)-1]
				confirmMsg += fmt.Sprintf("\nLast activity: %s (%s ago)",
					lastMsg.DataType,
					time.Since(lastMsg.Timestamp).Round(time.Second))
			}
			c.SendPrivateMessage(cmd.Source.User.Id, confirmMsg)

		} else if subType == "zip" {
			c.subscriptions.SubscribeToZip(cmd.Source.User.Id, code)
			c.SendMessage(cmd.Source.ChannelId, fmt.Sprintf("Subscribed to zip code %s", code))
		} else {
			c.SendMessage(cmd.Source.ChannelId, "Invalid subscription type. Use 'station' or 'zip'")
		}

	case "unsubscribe":
		if len(args) < 2 {
			c.SendMessage(cmd.Source.ChannelId, "Usage: !noaa unsubscribe <station|zip|all> [code]")
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
			c.SendMessage(cmd.Source.ChannelId, "Usage: !noaa unsubscribe <station|zip> <code>")
			return
		}
		code := args[2]

		if subType == "station" {
			if c.subscriptions.UnsubscribeFromStation(cmd.Source.User.Id, code) {
				c.SendMessage(cmd.Source.ChannelId, fmt.Sprintf("Unsubscribed from station %s", strings.ToUpper(code)))
			} else {
				c.SendMessage(cmd.Source.ChannelId, fmt.Sprintf("Not subscribed to station %s", strings.ToUpper(code)))
			}
		} else if subType == "zip" {
			if c.subscriptions.UnsubscribeFromZip(cmd.Source.User.Id, code) {
				c.SendMessage(cmd.Source.ChannelId, fmt.Sprintf("Unsubscribed from zip code %s", code))
			} else {
				c.SendMessage(cmd.Source.ChannelId, fmt.Sprintf("Not subscribed to zip code %s", code))
			}
		} else {
			c.SendMessage(cmd.Source.ChannelId, "Invalid subscription type. Use 'station', 'zip', or 'all'")
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
		zips := c.subscriptions.GetUserZipSubscriptions(cmd.Source.User.Id)

		msg := "Your subscriptions:\n"
		if len(stations) > 0 {
			msg += fmt.Sprintf("Stations: %s\n", strings.Join(stations, ", "))
		}
		if len(zips) > 0 {
			msg += fmt.Sprintf("Zip codes: %s\n", strings.Join(zips, ", "))
		}
		if len(stations) == 0 && len(zips) == 0 {
			msg = "You have no active subscriptions"
		}

		c.SendMessage(cmd.Source.ChannelId, msg)

	default:
		c.SendMessage(cmd.Source.ChannelId, "Unknown action. Use: subscribe, unsubscribe, or list")
	}
}

// Run runs both the NWWS client and seabird command handler concurrently
func (c *SeabirdClient) Run() error {
	g, _ := errgroup.WithContext(context.Background())

	log.Info().Msg("Starting NWWS-IO client")
	g.Go(func() error {
		return c.NWWSClient.Run()
	})

	log.Info().Msg("Starting seabird command handler")
	g.Go(func() error {
		c.handleCommandEvents()
		return nil
	})

	return g.Wait()
}
