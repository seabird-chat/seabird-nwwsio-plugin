package client

import (
	"fmt"
	"runtime"

	"github.com/rs/zerolog/log"
	"github.com/seabird-chat/seabird-go"
	nwwsio "github.com/seabird-chat/seabird-nwwsio-plugin/internal"
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

// SeabirdClient is a basic client for seabird
type SeabirdClient struct {
	*seabird.Client
	NWWSClient     *xmpp.StreamManager
	nwwsXMPPClient *xmpp.Client
	mucJID         *stanza.Jid
}

// NewSeabirdClient returns a new seabird client
func NewSeabirdClient(seabirdCoreURL, seabirdCoreToken, nwwsioUsername, nwwsioPassword string) (*SeabirdClient, error) {
	log.Info().Str("url", seabirdCoreURL).Msg("Connecting to seabird-core")
	seabirdClient, err := seabird.NewClient(seabirdCoreURL, seabirdCoreToken)
	if err != nil {
		return nil, err
	}
	log.Info().Str("url", seabirdCoreURL).Msg("Successfully connected to seabird-core")

	mucJID := &stanza.Jid{
		Node:     "nwws",
		Domain:   "conference.nwws-oi.weather.gov",
		Resource: nwwsioUsername,
	}

	log.Info().Str("username", nwwsioUsername).Msg("Connecting to NWWS-IO")
	nwwsioClient, nwwsXMPPClient, err := NewNWWSIOClient(nwwsioUsername, nwwsioPassword, mucJID)
	if err != nil {
		return nil, err
	}
	log.Info().Str("username", nwwsioUsername).Msg("Successfully connected to NWWS-IO")

	return &SeabirdClient{
		Client:         seabirdClient,
		NWWSClient:     nwwsioClient,
		nwwsXMPPClient: nwwsXMPPClient,
		mucJID:         mucJID,
	}, nil
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
func getAvailableNWWSIOSite(nwwsioUsername, nwwsioPassword string) (onlineNWWSIOConfig *xmpp.Config, err error) {
	router := xmpp.NewRouter()
	config := xmpp.Config{
		Jid:            fmt.Sprintf("%s@%s/%s", nwwsioUsername, NWWSDomain, NWWSResource),
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
func NewNWWSIOClient(nwwsioUsername, nwwsioPassword string, mucJID *stanza.Jid) (*xmpp.StreamManager, *xmpp.Client, error) {
	onlineClientConfig, err := getAvailableNWWSIOSite(nwwsioUsername, nwwsioPassword)
	if err != nil {
		return nil, nil, err
	}

	router := xmpp.NewRouter()
	router.HandleFunc("message", handleMessage)
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
		log.Info().Msg("The message stream from the NWWS-IO will begin now")
		err := joinMUC(c, mucJID)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to join Multi-user Chat")
		}
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

func handleMessage(s xmpp.Sender, p stanza.Packet) {
	msg, ok := p.(stanza.Message)
	if !ok {
		log.Debug().Str("type", fmt.Sprintf("%T", p)).Msg("Ignoring packet")
		return
	}

	log.Debug().Str("format", msg.XMPPFormat()).Msg("Message Debug Info")

	var messageNWWSIOX nwwsio.NWWSOIMessageXExtension
	if ok := msg.Get(&messageNWWSIOX); ok {
		productID, err := messageNWWSIOX.ParseTtaaii()
		if err != nil {
			log.Warn().Err(err).Str("ttaaii", messageNWWSIOX.Ttaaii).Msg("Failed to parse WMO product ID")
		} else {
			log.Info().
				Str("cccc", messageNWWSIOX.Cccc).
				Str("ttaaii", messageNWWSIOX.Ttaaii).
				Str("data_type", productID.GetDataType()).
				Str("awipsid", messageNWWSIOX.AwipsID).
				Str("issue", messageNWWSIOX.Issue).
				Msg("Received weather product")
		}
	}
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

// Run runs
func (c *SeabirdClient) Run() error {
	return c.NWWSClient.Run()
}
