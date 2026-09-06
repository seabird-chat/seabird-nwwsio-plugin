package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"
	"golang.org/x/sync/errgroup"
	"gosrc.io/xmpp"
	"gosrc.io/xmpp/stanza"
)

// Version is injected at build time with -ldflags "-X ...client.Version=vX.Y.Z".
var Version = "v0.0.0-dev"

type Config struct {
	SeabirdCoreURL   string
	SeabirdCoreToken string
	NWWSIOUsername   string
	NWWSIOPassword   string
	SubscriptionFile string // empty disables persistence
	// FilterTestMessages drops NWS test traffic (NTXX98/99 keepalives, TST
	// products, CAP alerts with status Test or Exercise) before it is logged,
	// recorded or delivered.
	FilterTestMessages bool
}

type SeabirdClient struct {
	*seabird.Client
	NWWSClient         *xmpp.StreamManager
	nwwsXMPPClient     *xmpp.Client
	mucJID             *stanza.Jid
	muc                *mucMonitor
	subscriptions      *SubscriptionManager
	filterTestMessages bool
	nwwsRunning        atomic.Bool
	cancelFunc         context.CancelFunc

	sequenceMu   sync.Mutex
	lastSequence map[string]int // NWWS ingest process ID -> last sequence number seen
}

func NewSeabirdClient(cfg Config) (*SeabirdClient, error) {
	log.Info().Str("url", cfg.SeabirdCoreURL).Msg("Connecting to seabird-core")
	seabirdClient, err := seabird.NewClient(cfg.SeabirdCoreURL, cfg.SeabirdCoreToken)
	if err != nil {
		return nil, err
	}
	log.Info().Str("url", cfg.SeabirdCoreURL).Msg("Successfully connected to seabird-core")

	instanceID := generateInstanceID()
	client := &SeabirdClient{
		Client: seabirdClient,
		mucJID: &stanza.Jid{
			Node:     "nwws",
			Domain:   "conference.nwws-oi.weather.gov",
			Resource: fmt.Sprintf("%s-%s", cfg.NWWSIOUsername, instanceID),
		},
		subscriptions:      NewSubscriptionManager(),
		lastSequence:       make(map[string]int),
		filterTestMessages: cfg.FilterTestMessages,
	}
	log.Info().Str("instance_id", instanceID).Bool("filter_test_messages", cfg.FilterTestMessages).Msg("Client configured")

	if cfg.SubscriptionFile != "" {
		client.subscriptions.SetPersistenceFile(cfg.SubscriptionFile)
		if err := client.subscriptions.Load(); err != nil {
			log.Error().Err(err).Msg("Failed to load subscriptions from file")
		}
	} else {
		log.Warn().Msg("No subscription file configured - subscriptions will not persist across restarts")
	}

	log.Info().Str("username", cfg.NWWSIOUsername).Msg("Connecting to NWWS-IO")
	streamManager, xmppClient, err := newNWWSClient(cfg.NWWSIOUsername, cfg.NWWSIOPassword, instanceID, client)
	if err != nil {
		return nil, err
	}
	log.Info().Str("username", cfg.NWWSIOUsername).Msg("Successfully connected to NWWS-IO")

	client.NWWSClient = streamManager
	client.nwwsXMPPClient = xmppClient
	return client, nil
}

// Run blocks until Shutdown is called or a component fails permanently.
func (c *SeabirdClient) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancelFunc = cancel
	defer cancel()

	g, gctx := errgroup.WithContext(ctx)

	log.Info().Msg("Starting NWWS-IO client")
	c.nwwsRunning.Store(true)
	g.Go(func() error {
		err := c.NWWSClient.Run()
		c.nwwsRunning.Store(false)
		return err
	})

	log.Info().Msg("Starting seabird command handler")
	g.Go(func() error {
		return c.handleCommandEvents(gctx)
	})

	g.Go(func() error {
		c.muc.watchdog(gctx)
		return nil
	})

	// If anything above fails, stop the NWWS client so Run returns and the
	// process exits instead of running half-alive.
	g.Go(func() error {
		<-gctx.Done()
		c.stopNWWS()
		return nil
	})

	return g.Wait()
}

func (c *SeabirdClient) Shutdown() error {
	log.Info().Msg("Shutting down gracefully")

	// Stop the room monitor first so our own leave is not treated as a kick.
	if c.muc != nil {
		c.muc.stop()
	}

	if c.nwwsXMPPClient != nil && c.mucJID != nil {
		err := c.nwwsXMPPClient.Send(stanza.Presence{
			Attrs: stanza.Attrs{To: c.mucJID.Full(), Type: stanza.PresenceTypeUnavailable},
		})
		if err != nil {
			log.Error().Err(err).Msg("Failed to send presence unavailable")
		}
	}

	if c.cancelFunc != nil {
		c.cancelFunc()
	}

	if c.subscriptions != nil {
		if err := c.subscriptions.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to save subscriptions during shutdown")
		}
	}

	c.stopNWWS()

	if c.Client != nil {
		return c.Client.Close()
	}
	return nil
}

// stopNWWS runs at most once and only while the stream manager is running;
// Stop on a manager whose Run already returned panics on its WaitGroup.
func (c *SeabirdClient) stopNWWS() {
	if c.NWWSClient != nil && c.nwwsRunning.CompareAndSwap(true, false) {
		c.NWWSClient.Stop()
	}
}

func (c *SeabirdClient) SendMessage(channelID, text string) {
	_, err := c.Client.Inner.SendMessage(context.Background(), &pb.SendMessageRequest{
		ChannelId: channelID,
		Text:      text,
	})
	if err != nil {
		log.Error().Err(err).Str("channel_id", channelID).Msg("Failed to send message")
	}
}

func (c *SeabirdClient) SendPrivateMessage(userID, text string) {
	_, err := c.Client.Inner.SendPrivateMessage(context.Background(), &pb.SendPrivateMessageRequest{
		UserId: userID,
		Text:   text,
	})
	if err != nil {
		log.Error().Err(err).Str("user_id", userID).Msg("Failed to send private message")
	}
}

func generateInstanceID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().Unix()%10000)
	}
	return hex.EncodeToString(b)
}
