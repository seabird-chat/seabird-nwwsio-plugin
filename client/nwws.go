package client

import (
	"fmt"
	"runtime"
	"time"

	"github.com/rs/zerolog/log"
	"gosrc.io/xmpp"
	"gosrc.io/xmpp/stanza"
)

// NWWS-IO servers, see https://www.weather.gov/nwws/#access
const (
	NWWSCollegePark   = "nwws-oi-cprk.weather.gov"
	NWWSBoulder       = "nwws-oi-bldr.weather.gov"
	NWWSServerPort    = "5222"
	NWWSDomain        = "nwws-oi.weather.gov"
	NWWSResource      = "nwws"
	ConnectionTimeout = 3 * time.Second
)

// newNWWSClient finds a reachable NWWS-IO site, then builds the XMPP client
// and the stream manager that reconnects it and rejoins the room.
func newNWWSClient(username, password, instanceID string, client *SeabirdClient) (*xmpp.StreamManager, *xmpp.Client, error) {
	config, err := findReachableNWWSSite(username, password, instanceID)
	if err != nil {
		return nil, nil, err
	}

	monitor := newMUCMonitor(client.mucJID)
	client.muc = monitor

	router := xmpp.NewRouter()
	router.HandleFunc("message", func(_ xmpp.Sender, p stanza.Packet) { handleMessage(p, client) })
	router.HandleFunc("presence", func(_ xmpp.Sender, p stanza.Packet) { handlePresence(p, monitor) })
	router.NewRoute().IQNamespaces("jabber:iq:version").HandlerFunc(handleVersion)

	xmppClient, err := xmpp.NewClient(config, router, xmppErrorHandler)
	if err != nil {
		return nil, nil, err
	}
	monitor.disconnect = func() {
		if err := xmppClient.Disconnect(); err != nil {
			log.Error().Err(err).Msg("Forced NWWS-IO disconnect reported an error")
		}
	}

	streamManager := xmpp.NewStreamManager(xmppClient, func(s xmpp.Sender) { onNWWSConnected(s, monitor) })
	return streamManager, xmppClient, nil
}

// findReachableNWWSSite logs in to each site with a throwaway connection and
// returns the config for the first one that works.
func findReachableNWWSSite(username, password, instanceID string) (*xmpp.Config, error) {
	config := xmpp.Config{
		Jid:            fmt.Sprintf("%s@%s/%s-%s", username, NWWSDomain, NWWSResource, instanceID),
		Credential:     xmpp.Password(password),
		ConnectTimeout: int(ConnectionTimeout.Seconds()),
	}

	for _, host := range []string{NWWSCollegePark, NWWSBoulder} {
		config.TransportConfiguration = xmpp.TransportConfiguration{
			Address: host + ":" + NWWSServerPort,
			Domain:  NWWSDomain,
		}
		probe, err := xmpp.NewClient(&config, xmpp.NewRouter(), probeErrorHandler)
		if err != nil {
			return nil, err
		}

		log.Info().Str("site", config.Address).Msg("Testing connection to NWWS-IO site")
		if err := probe.Connect(); err != nil {
			log.Warn().Err(err).Str("site", host).Msg("NWWS-IO site unreachable, trying next")
			_ = probe.Disconnect()
			continue
		}
		if err := probe.Disconnect(); err != nil {
			return nil, err
		}
		return &config, nil
	}
	return nil, fmt.Errorf("failed to connect to any NWWS-IO site")
}

func onNWWSConnected(s xmpp.Sender, monitor *mucMonitor) {
	log.Info().Msg("NWWS-IO connection established")
	if err := joinMUC(s, monitor.mucJID); err != nil {
		log.Fatal().Err(err).Msg("Failed to join Multi-user Chat")
	}
	monitor.connected(s)
	log.Info().Str("jid", monitor.mucJID.Full()).Msg("Sent Multi-user Chat join - ready to receive messages")
}

func joinMUC(s xmpp.Sender, room *stanza.Jid) error {
	log.Info().Str("jid", room.Full()).Msg("Joining Multi-user Chat")
	return s.Send(stanza.Presence{
		Attrs: stanza.Attrs{To: room.Full()},
		Extensions: []stanza.PresExtension{
			stanza.MucPresence{History: stanza.History{MaxStanzas: stanza.NewNullableInt(0)}},
		},
	})
}

// The probe's receive loop reports its own teardown as an error once it is
// disconnected, so its errors are only worth a debug line.
func probeErrorHandler(err error) {
	log.Debug().Err(err).Msg("XMPP error on site probe connection")
}

func xmppErrorHandler(err error) {
	log.Error().Err(err).Msg("XMPP error")
}

func handleVersion(s xmpp.Sender, p stanza.Packet) {
	iq, ok := p.(*stanza.IQ)
	if !ok {
		return
	}
	resp, err := stanza.NewIQ(stanza.Attrs{Type: "result", From: iq.To, To: iq.From, Id: iq.Id, Lang: "en"})
	if err != nil {
		return
	}
	resp.Version().SetInfo("seabird-nwwsio-plugin", Version, fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH))
	_ = s.Send(resp)
}
