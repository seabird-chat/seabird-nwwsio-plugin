module github.com/seabird-chat/seabird-nwwsio-plugin

go 1.26

require (
	github.com/mattn/go-isatty v0.0.24
	github.com/rs/zerolog v1.35.1
	github.com/seabird-chat/seabird-go v0.6.1
	golang.org/x/sync v0.22.0
	google.golang.org/grpc v1.83.2
	gosrc.io/xmpp v0.5.2-0.20260819202247-98830ceeb5d2
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260831171406-18b4a7587f8a // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	nhooyr.io/websocket v1.8.17 // indirect
)

replace gosrc.io/xmpp => github.com/jaredledvina/go-xmpp v0.0.0-20260906030935-5766e48a7d9b
