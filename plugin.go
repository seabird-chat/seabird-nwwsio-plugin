package client

import (
	"encoding/xml"
	"fmt"
	"log"
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
	log.Printf("Testing connection to NWWS-IO site: %s", config.Address)
	err = client.Connect()
	if err != nil {
		log.Printf("Failed to connect to NWWS-IO server %s, trying %s. Error: %+v", NWWSCollegePark, NWWSBoulder, err)
		boulderConfig := xmpp.TransportConfiguration{
			Address: fmt.Sprintf("%s:%s", NWWSBoulder, NWWSServerPort),
			Domain:  NWWSDomain,
		}
		config.TransportConfiguration = boulderConfig

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
	// TODO
	// On Exit:
	// c.Send(stanza.Presence{Attrs: stanza.Attrs{
	//		To:   toJID.Full(),
	//		Type: stanza.PresenceTypeUnavailable,
	//	}}
	cm := xmpp.NewStreamManager(onlineClient, nwwsioPostConnect)
	return cm, nil
}

func nwwsioPostConnect(c xmpp.Sender) {
	log.Println("The message stream from the NWWS-IO will begin now...")
	err := joinMUC(c, &stanza.Jid{
		Node:   "nwws",
		Domain: "conference.nwws-oi.weather.gov",
		//TODO: This should be nwwsioUsername
		Resource: "wind.060",
	})
	if err != nil {
		log.Fatalf("Failed to join Multi-user Chat: %v", err)
	}

}

func joinMUC(c xmpp.Sender, toJID *stanza.Jid) error {
	log.Printf("Attempting to join Multi-user chat: %s", toJID.Full())
	return c.Send(stanza.Presence{Attrs: stanza.Attrs{To: toJID.Full()},
		Extensions: []stanza.PresExtension{
			stanza.MucPresence{
				History: stanza.History{MaxStanzas: stanza.NewNullableInt(0)},
			}},
	})
}

/*
Documentation:
* https://www.weather.gov/nwws/configuration
* https://www.weather.gov/tg/head

Example Message Format:
<message to='enduser@server/laptop' type='groupchat' from='nwws@nwws-oi.weather.gov/nwws-oi'>

<body>KARX issues RR8 valid 2013-05-25T02:20:34Z</body>

<html xmlns='http://jabber.org/protocol/xhtml-im'>

<body xmlns='http://www.w3.org/1999/xhtml'>KARX issues RR8 valid 2013-05-25T02:20:34Z</body>

</html>

<x xmlns='nwws-oi' cccc='KARX' ttaaii='SRUS83' issue='2013-05-25T02:20:34Z' awipsid='RR8ARX' id='10313.6'>

111

# SRUS83 KARX 250220

# RR8ARX

:

: AUTOMATED GAUGE DATA COLLECTED FROM IOWA FLOOD CENTER

:

.A CDGI4 20130524 C DH2100/HGIRP 2.63 : MORGAN CREEK NEAR CEDAR RAPIDS

</x>

</message>
*/

type NWWSOIMessageXExtension struct {
	stanza.MsgExtension
	XMLName xml.Name `xml:"nwws-oi x"`
	Text    string   `xml:",chardata"`
	Cccc    string   `xml:"cccc,attr"`
	Ttaaii  string   `xml:"ttaaii,attr"`
	Issue   string   `xml:"issue,attr"`
	AwipsID string   `xml:"awipsid,attr"`
	ID      string   `xml:"id,attr"`
}

func init() {
	stanza.TypeRegistry.MapExtension(stanza.PKTMessage, xml.Name{Space: "nwws-oi", Local: "x"}, NWWSOIMessageXExtension{})
}

func handleMessage(s xmpp.Sender, p stanza.Packet) {
	msg, ok := p.(stanza.Message)
	if !ok {
		log.Printf("Ignoring packet: %T", p)
		return
	}

	log.Printf("Message received - From: %s Body: %s", msg.From, msg.Body)
	log.Printf("Message Debug Info: %s", msg.XMPPFormat())
	var messageNWWSIOX NWWSOIMessageXExtension
	if ok := msg.Get(&messageNWWSIOX); ok {
		log.Printf("Message X Text: %v", messageNWWSIOX.Text)
	}
	xmlMsg, err := xml.Marshal(msg)
	if err != nil {
		fmt.Printf("ERROR: Failed to marshal message: %s", msg.Id)
	}
	fmt.Println("=========== XML Formatted START ===========")
	fmt.Println(xmlfmt.FormatXML(string(xmlMsg), "\t", "  "))
	fmt.Println("=========== XML Formatted END ===========")
}

func errorHandler(err error) {
	log.Printf("ERROR: %v", err)
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
