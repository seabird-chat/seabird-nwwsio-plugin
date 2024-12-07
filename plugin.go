package client

import (
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/go-xmlfmt/xmlfmt"
	"github.com/seabird-chat/seabird-go"
	"gosrc.io/xmpp"
	"gosrc.io/xmpp/stanza"
)

const (
	// NWWS Servers, see https://www.weather.gov/nwws/#access
	NWWSBoulder     string = "nwws-oi-bldr.weather.gov"
	NWWSCollegePark string = "nwws-oi-cprk.weather.gov"
	// NWWSServerPort is the port to connect to via XMPP
	NWWSServerPort string = "5222"
	NWWSDomain     string = "nwws-oi.weather.gov"
	NWWSResource   string = "nwws"
)

var Version = "v0.0.0-dev"

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
	xmlMsg, err := xml.Marshal(msg)
	if err != nil {
		fmt.Println("ERROR: Failed to marshal message")
	}
	fmt.Println(xmlfmt.FormatXML(string(xmlMsg), "\t", "  "))
	// Disabled - We don't want to reply...just listen
	//reply := stanza.Message{Attrs: stanza.Attrs{To: msg.From}, Body: msg.Body}
	//_ = s.Send(reply)
}

func errorHandler(err error) {
	fmt.Println(err.Error())
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

// NewNWWSIOClient returns a new NWWS-IO Client
func NewNWWSIOClient(nwwsioUsername, nwwsioPassword string) (*xmpp.StreamManager, error) {
	config := xmpp.Config{
		TransportConfiguration: xmpp.TransportConfiguration{
			Address: fmt.Sprintf("%s:%s", NWWSCollegePark, NWWSServerPort),
			Domain:  NWWSDomain,
		},
		Jid:          fmt.Sprintf("%s@%s/%s", nwwsioUsername, NWWSDomain, NWWSResource),
		Credential:   xmpp.Password(nwwsioPassword),
		StreamLogger: os.Stdout,
		Insecure:     false,
	}

	router := xmpp.NewRouter()
	router.HandleFunc("message", handleMessage)
	router.NewRoute().IQNamespaces("jabber:iq:version").HandlerFunc(handleVersion)

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
