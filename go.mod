module github.com/seabird-chat/seabird-nwwsio-plugin

go 1.26.2

require (
	github.com/joho/godotenv v1.5.1
	github.com/mattn/go-isatty v0.0.20
	github.com/rs/zerolog v1.34.0
	github.com/seabird-chat/seabird-go v0.6.1
	golang.org/x/sync v0.21.0
	google.golang.org/grpc v1.79.3
	gosrc.io/xmpp v0.5.1
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.32.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	nhooyr.io/websocket v1.8.17 // indirect
)

replace gosrc.io/xmpp => github.com/jaredledvina/go-xmpp v0.0.0-20250412144549-ab19715da354
