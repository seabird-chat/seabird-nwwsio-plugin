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
	NWWSServerPort  string = "5222"
	NWWSDomain      string = "nwws-oi.weather.gov"
	NWWSResource    string = "nwws"
)

var Version = "v0.0.0-dev"

// SeabirdClient is a basic client for seabird
type SeabirdClient struct {
	*seabird.Client
	NWWSClient *xmpp.StreamManager
}

// NewSeabirdClient returns a new seabird client
func NewSeabirdClient(seabirdCoreURL, seabirdCoreToken, nwwsioUsername, nwwsioPassword string) (*SeabirdClient, error) {
	log.Printf("Connecting to seabird-core: %s", seabirdCoreURL)
	seabirdClient, err := seabird.NewClient(seabirdCoreURL, seabirdCoreToken)
	if err != nil {
		return nil, err
	}
	log.Printf("Succesfully connected to seabird-core: %s", seabirdCoreURL)

	log.Printf("Connecting to NWWS-IO as: %s", nwwsioUsername)
	nwwsioClient, err := NewNWWSIOClient(nwwsioUsername, nwwsioPassword)
	if err != nil {
		return nil, err
	}
	log.Printf("Succesfully connected to NWWS-IO as: %s", nwwsioUsername)

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

// getAvailableNWWSIOSite attempts to connect to college park & boulder NWWS-IO
// sites and will return an XMPP client for the first successful site.
func getAvailableNWWSIOSite(nwwsioUsername, nwwsioPassword string) (onlineNWWSIOConfig *xmpp.Config, err error) {
	router := xmpp.NewRouter()
	collegeParkConfig := xmpp.TransportConfiguration{
		Address: fmt.Sprintf("%s:%s", NWWSCollegePark, NWWSServerPort),
		Domain:  NWWSDomain,
	}

	config := xmpp.Config{
		TransportConfiguration: collegeParkConfig,
		Jid:                    fmt.Sprintf("%s@%s/%s", nwwsioUsername, NWWSDomain, NWWSResource),
		Credential:             xmpp.Password(nwwsioPassword),
		Insecure:               false,
		ConnectTimeout:         3,
	}

	client, err := xmpp.NewClient(&config, router, errorHandler)
	if err != nil {
		return nil, err
	}
	log.Printf("Testing connection to NWWS-IO site: %s", config.Address)
	err = client.Connect()
	if err != nil {
		log.Printf("Failed to connect to NWWS-IO server %s, trying %s. Error: %+v", NWWSCollegePark, NWWSBoulder, err)
		boulderConfig := xmpp.TransportConfiguration{
			Address: fmt.Sprintf("%s:%s", NWWSBoulder, NWWSServerPort),
			Domain:  NWWSDomain,
		}
		config = xmpp.Config{
			TransportConfiguration: boulderConfig,
			Jid:                    fmt.Sprintf("%s@%s/%s", nwwsioUsername, NWWSDomain, NWWSResource),
			Credential:             xmpp.Password(nwwsioPassword),
			Insecure:               false,
			ConnectTimeout:         3,
		}

		client, err = xmpp.NewClient(&config, router, errorHandler)
		if err != nil {
			return nil, err
		}

		log.Printf("Testing connection to NWWS-IO site: %s", config.Address)
		err = client.Connect()
		if err != nil {
			log.Println("Failed to connect to all NWWS-IO sites")
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
func NewNWWSIOClient(nwwsioUsername, nwwsioPassword string) (*xmpp.StreamManager, error) {
	onlineClientConfig, err := getAvailableNWWSIOSite(nwwsioUsername, nwwsioPassword)
	if err != nil {
		return nil, err
	}

	router := xmpp.NewRouter()
	router.HandleFunc("message", handleMessage)
	router.NewRoute().IQNamespaces("jabber:iq:version").HandlerFunc(handleVersion)

	onlineClient, err := xmpp.NewClient(onlineClientConfig, router, errorHandler)
	if err != nil {
		return nil, err
	}

	cm := xmpp.NewStreamManager(onlineClient, nwwsioPostConnect)
	return cm, nil
}

func nwwsioPostConnect(c xmpp.Sender) {
	log.Println("The message stream from the NWWS-IO will begin now...")
}

// Run runs
func (c *SeabirdClient) Run() error {
	return c.NWWSClient.Run()
}
