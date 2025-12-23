module github.com/seabird-chat/seabird-nwwsio-plugin

go 1.25.5

require (
	github.com/joho/godotenv v1.5.1
	github.com/mattn/go-isatty v0.0.20
	github.com/rs/zerolog v1.34.0
	github.com/seabird-chat/seabird-go v0.6.1
	golang.org/x/sync v0.18.0
	gosrc.io/xmpp v0.5.1
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	golang.org/x/net v0.39.0 // indirect
	golang.org/x/sys v0.32.0 // indirect
	golang.org/x/text v0.24.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250409194420-de1ac958c67a // indirect
	google.golang.org/grpc v1.71.1 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
	nhooyr.io/websocket v1.8.17 // indirect
)

replace gosrc.io/xmpp => github.com/jaredledvina/go-xmpp v0.0.0-20250412144549-ab19715da354
