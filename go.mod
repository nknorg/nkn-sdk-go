module github.com/nknorg/nkn-sdk-go

go 1.12

require (
	github.com/gogo/protobuf v1.2.1
	github.com/gorilla/websocket v1.4.0
	github.com/hashicorp/go-multierror v1.0.0
	github.com/nknorg/nkn v1.0.2-beta
	github.com/pkg/errors v0.8.1
)

replace github.com/nknorg/nkn => github.com/trueinsider/nkn v0.5.3-alpha.0.20190828053242-af586b32fef3
