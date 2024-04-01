package client

import (
	"fmt"
	"log"
	"os"

	"github.com/seabird-chat/seabird-go"
	"gosrc.io/xmpp"
	"gosrc.io/xmpp/stanza"
)

const (
	// NWWSPrimaryIP is static, see https://www.weather.gov/nwws/#access
	NWWSPrimaryIP string = "140.90.59.197"
	// NWWSSecondaryIP is static, see https://www.weather.gov/nwws/#access
	NWWSSecondaryIP string = "140.90.113.240"
	// NWWSServerPort is the port to connect to via XMPP
	NWWSServerPort string = "5223"
)

// SeabirdClient is a basic client for seabird
type SeabirdClient struct {
	*seabird.Client
	NWWSClient *xmpp.StreamManager
}

// NewSeabirdClient returns a new seabird client
func NewSeabirdClient(seabirdCoreURL, seabirdCoreToken, nwwsioUsername, nwwsioPassword string) (*SeabirdClient, error) {
	seabirdClient, _ := seabird.NewClient(seabirdCoreURL, seabirdCoreToken)
	//if err != nil {
	//	return nil, err
	//}
	nwwsioClient, err := NewNWWSIOClient(nwwsioUsername, nwwsioPassword)
	if err != nil {
		return nil, err
	}

	return &SeabirdClient{
		Client:     seabirdClient,
		NWWSClient: nwwsioClient,
	}, nil
}

func (c *SeabirdClient) close() error {
	return c.Client.Close()
}

func handleMessage(s xmpp.Sender, p stanza.Packet) {
	msg, ok := p.(stanza.Message)
	if !ok {
		_, _ = fmt.Fprintf(os.Stdout, "Ignoring packet: %T\n", p)
		return
	}

	_, _ = fmt.Fprintf(os.Stdout, "Body = %s - from = %s\n", msg.Body, msg.From)
	reply := stanza.Message{Attrs: stanza.Attrs{To: msg.From}, Body: msg.Body}
	_ = s.Send(reply)
}

func errorHandler(err error) {
	fmt.Println(err.Error())
}

// NewNWWSIOClient returns a new NWWS-IO Client
func NewNWWSIOClient(nwwsioUsername, nwwsioPassword string) (*xmpp.StreamManager, error) {
	config := xmpp.Config{
		TransportConfiguration: xmpp.TransportConfiguration{
			Address: fmt.Sprintf("%s:%s", NWWSPrimaryIP, NWWSServerPort),
		},
		Jid:          nwwsioUsername,
		Credential:   xmpp.Password(nwwsioPassword),
		StreamLogger: os.Stdout,
		Insecure:     true,
	}

	router := xmpp.NewRouter()
	router.HandleFunc("message", handleMessage)

	client, err := xmpp.NewClient(&config, router, errorHandler)
	if err != nil {
		log.Fatalf("%+v", err)
	}

	// If you pass the client to a connection manager, it will handle the reconnect policy
	// for you automatically.
	cm := xmpp.NewStreamManager(client, nil)
	return cm, nil
}

// Run runs
func (c *SeabirdClient) Run() error {
	log.Fatal(c.NWWSClient.Run())
	return nil
}
