package nkn

import (
	"context"
	"math/rand"
	"net"
	"time"

	"github.com/imdario/mergo"
	ncp "github.com/nknorg/ncp-go"
	"github.com/nknorg/nkngomobile"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// DefaultSeedRPCServerAddr is the default seed rpc server address list.
var DefaultSeedRPCServerAddr = []string{
	"http://seed.nkn.org:30003",
	"http://mainnet-seed-0001.nkn.org:30003",
	"http://mainnet-seed-0002.nkn.org:30003",
	"http://mainnet-seed-0003.nkn.org:30003",
	"http://mainnet-seed-0004.nkn.org:30003",
	"http://mainnet-seed-0005.nkn.org:30003",
	"http://mainnet-seed-0006.nkn.org:30003",
	"http://mainnet-seed-0007.nkn.org:30003",
	"http://mainnet-seed-0008.nkn.org:30003",
}

var DefaultStunServerAddr = []string{
	"stun:stun.l.google.com:19302",
	"stun:stun.cloudflare.com:3478",
	"stun:stunserver.stunprotocol.org:3478",
}

// ClientConfig is the client configuration.
type ClientConfig struct {
	SeedRPCServerAddr       *nkngomobile.StringArray // Seed RPC server address that client uses to find its node and make RPC requests (e.g. get subscribers).
	RPCTimeout              int32                    // Timeout for each RPC call in millisecond
	RPCConcurrency          int32                    // If greater than 1, the same rpc request will be concurrently sent to multiple seed rpc nodes
	MsgChanLen              int32                    // Channel length for received but unproccessed messages.
	ConnectRetries          int32                    // Connnect to node retries (including the initial connect). A negative value means unlimited retries.
	MsgCacheExpiration      int32                    // Message cache expiration in millisecond for response channel, multiclient message id deduplicate, etc.
	MsgCacheCleanupInterval int32                    // Message cache cleanup interval in millisecond.
	WsHandshakeTimeout      int32                    // WebSocket handshake timeout in millisecond.
	WsWriteTimeout          int32                    // WebSocket write timeout in millisecond.
	MinReconnectInterval    int32                    // Min reconnect interval in millisecond.
	MaxReconnectInterval    int32                    // Max reconnect interval in millisecond.
	AllowUnencrypted        bool                     // Allow receiving unencrypted message. Unencrypted message might have sender or body viewed/modified by middleman or forged by sender.
	MessageConfig           *MessageConfig           // Default message config of the client if per-message config is not provided.
	SessionConfig           *ncp.Config              // Default session config of the client if per-session config is not provided.

	HttpDialContext func(ctx context.Context, network, addr string) (net.Conn, error) // Customized http dialcontext function
	WsDialContext   func(ctx context.Context, network, addr string) (net.Conn, error) // Customized websocket dialcontext function

	Resolvers       *nkngomobile.ResolverArray // Resolvers resolve a string to a NKN address
	ResolverDepth   int32                      // Max recursive resolve calls
	ResolverTimeout int32                      // Timeout for the whole resolving process

	WebRTC               bool                     // Whether to use webrtc for data channel
	StunServerAddr       *nkngomobile.StringArray // Stun server address that client uses to find its node
	WebRTCConnectTimeout int32                    // Timeout for webrtc connection in milliseconds
}

// DefaultClientConfig is the default client config.
var DefaultClientConfig = ClientConfig{
	SeedRPCServerAddr:       nil,
	RPCTimeout:              10000,
	RPCConcurrency:          1,
	MsgChanLen:              1024,
	ConnectRetries:          3,
	MsgCacheExpiration:      300000,
	MsgCacheCleanupInterval: 60000,
	WsHandshakeTimeout:      5000,
	WsWriteTimeout:          10000,
	MinReconnectInterval:    1000,
	MaxReconnectInterval:    64000,
	AllowUnencrypted:        false,
	MessageConfig:           nil,
	SessionConfig:           nil,
	HttpDialContext:         nil,
	WsDialContext:           nil,
	Resolvers:               nil,
	ResolverDepth:           16,
	ResolverTimeout:         10000,
	WebRTC:                  false,
	StunServerAddr:          nil,
	WebRTCConnectTimeout:    10000,
}

// GetDefaultClientConfig returns the default client config with nil pointer
// fields set to default.
func GetDefaultClientConfig() *ClientConfig {
	clientConf := DefaultClientConfig
	clientConf.SeedRPCServerAddr = NewStringArray(DefaultSeedRPCServerAddr...)
	clientConf.MessageConfig = GetDefaultMessageConfig()
	clientConf.SessionConfig = GetDefaultSessionConfig()
	clientConf.StunServerAddr = NewStringArray(DefaultStunServerAddr...)
	return &clientConf
}

// RPCGetSeedRPCServerAddr returns all seed rpc server addresses. RPC prefix is
// added to avoid gomobile compile error.
func (c *ClientConfig) RPCGetSeedRPCServerAddr() *nkngomobile.StringArray {
	return c.SeedRPCServerAddr
}

// RPCGetRPCTimeout returns RPC timeout in millisecond. RPC prefix is added to
// avoid gomobile compile error.
func (c *ClientConfig) RPCGetRPCTimeout() int32 {
	return c.RPCTimeout
}

// RPCGetConcurrency returns RPC concurrency. RPC prefix is added to avoid
// gomobile compile error.
func (c *ClientConfig) RPCGetConcurrency() int32 {
	return c.RPCConcurrency
}

func (c *ClientConfig) RPCGetHttpDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	return c.HttpDialContext
}

// MessageConfig is the config for sending messages.
type MessageConfig struct {
	Unencrypted       bool   // Whether message body should be unencrypted. It is not recommended to send unencrypted message as anyone in the middle can see the message content.
	NoReply           bool   // Indicating the message will not have any reply or ACK, so client will not allocate any resources waiting for it.
	MaxHoldingSeconds int32  // Message will be held at node for at most this time if the destination client is not online. Note that message might be released earlier than this time if node runs out of resources.
	MessageID         []byte // Message ID. If nil, a random ID will be generated for each message. MessageID should be unique per message and has size MessageIDSize.

	// for publish
	TxPool bool  // Whether to include subscribers in txpool when publishing.
	Offset int32 // Offset for getting subscribers.
	Limit  int32 // Single request limit for getting subscribers
}

// DefaultMessageConfig is the default message config.
var DefaultMessageConfig = MessageConfig{
	Unencrypted:       false,
	NoReply:           false,
	MaxHoldingSeconds: 0,
	MessageID:         nil,
	TxPool:            false,
	Offset:            0,
	Limit:             1000,
}

// GetDefaultMessageConfig returns the default message config.
func GetDefaultMessageConfig() *MessageConfig {
	messageConf := DefaultMessageConfig
	return &messageConf
}

// DefaultSessionConfig is the default session config. Unspecific fields here
// will use the default config in https://github.com/nknorg/ncp-go.
var DefaultSessionConfig = ncp.Config{
	MTU: 1024,
}

// GetDefaultSessionConfig returns the default session config.
func GetDefaultSessionConfig() *ncp.Config {
	sessionConf := DefaultSessionConfig
	return &sessionConf
}

// DialConfig is the dial config for session.
type DialConfig struct {
	DialTimeout   int32       // Dial timeout in millisecond
	SessionConfig *ncp.Config // Per-session session config that will override client session config.
}

// DefaultDialConfig is the default dial config.
var DefaultDialConfig = DialConfig{
	DialTimeout:   0,
	SessionConfig: nil,
}

// GetDefaultDialConfig returns the default dial config with nil pointer fields
// set to default.
func GetDefaultDialConfig(baseSessionConfig *ncp.Config) *DialConfig {
	dialConf := DefaultDialConfig
	sessionConfig := *baseSessionConfig
	dialConf.SessionConfig = &sessionConfig
	return &dialConf
}

// ScryptConfig is the scrypt configuration.
type ScryptConfig struct {
	Salt []byte
	N    int
	R    int
	P    int
}

// WalletConfig is the wallet configuration.
type WalletConfig struct {
	SeedRPCServerAddr *nkngomobile.StringArray
	RPCTimeout        int32 // Timeout for each RPC call in millisecond
	RPCConcurrency    int32 // If greater than 1, the same rpc request will be concurrently sent to multiple seed rpc nodes
	Password          string
	IV                []byte
	MasterKey         []byte
	ScryptConfig      *ScryptConfig
	HttpDialContext   func(ctx context.Context, network, addr string) (net.Conn, error)
}

// DefaultWalletConfig is the default wallet configuration.
var DefaultWalletConfig = WalletConfig{
	SeedRPCServerAddr: nil,
	RPCTimeout:        10000,
	RPCConcurrency:    1,
	IV:                nil,
	MasterKey:         nil,
	ScryptConfig:      nil,
	HttpDialContext:   nil,
}

// GetDefaultWalletConfig returns the default wallet config with nil pointer
// fields set to default.
func GetDefaultWalletConfig() *WalletConfig {
	walletConf := DefaultWalletConfig
	walletConf.SeedRPCServerAddr = nkngomobile.NewStringArray(DefaultSeedRPCServerAddr...)
	walletConf.ScryptConfig = &ScryptConfig{}
	return &walletConf
}

// RPCGetSeedRPCServerAddr returns all seed rpc server addresses. RPC prefix is
// added to avoid gomobile compile error.
func (c *WalletConfig) RPCGetSeedRPCServerAddr() *nkngomobile.StringArray {
	return c.SeedRPCServerAddr
}

// RPCGetRPCTimeout returns RPC timeout in millisecond. RPC prefix is added to
// avoid gomobile compile error.
func (c *WalletConfig) RPCGetRPCTimeout() int32 {
	return c.RPCTimeout
}

// RPCGetConcurrency returns RPC concurrency. RPC prefix is added to avoid
// gomobile compile error.
func (c *WalletConfig) RPCGetConcurrency() int32 {
	return c.RPCConcurrency
}

func (c *WalletConfig) RPCGetHttpDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	return c.HttpDialContext
}

// RPCConfig is the rpc call configuration.
type RPCConfig struct {
	SeedRPCServerAddr *nkngomobile.StringArray
	RPCTimeout        int32 // Timeout for each RPC call in millisecond
	RPCConcurrency    int32 // If greater than 1, the same rpc request will be concurrently sent to multiple seed rpc nodes
	HttpDialContext   func(ctx context.Context, network, addr string) (net.Conn, error)
}

// DefaultRPCConfig is the default rpc configuration.
var DefaultRPCConfig = RPCConfig{
	SeedRPCServerAddr: nil,
	RPCTimeout:        10000,
	RPCConcurrency:    1,
	HttpDialContext:   nil,
}

// GetDefaultRPCConfig returns the default rpc config with nil pointer fields
// set to default.
func GetDefaultRPCConfig() *RPCConfig {
	rpcConf := DefaultRPCConfig
	rpcConf.SeedRPCServerAddr = nkngomobile.NewStringArray(DefaultSeedRPCServerAddr...)
	return &rpcConf
}

// RPCGetSeedRPCServerAddr returns all seed rpc server addresses. RPC prefix is
// added to avoid gomobile compile error.
func (c *RPCConfig) RPCGetSeedRPCServerAddr() *nkngomobile.StringArray {
	return c.SeedRPCServerAddr
}

// RPCGetRPCTimeout returns RPC timeout in millisecond. RPC prefix is added to
// avoid gomobile compile error.
func (c *RPCConfig) RPCGetRPCTimeout() int32 {
	return c.RPCTimeout
}

// RPCGetConcurrency returns RPC concurrency. RPC prefix is added to avoid
// gomobile compile error.
func (c *RPCConfig) RPCGetConcurrency() int32 {
	return c.RPCConcurrency
}

func (c *RPCConfig) RPCGetHttpDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	return c.HttpDialContext
}

// TransactionConfig is the config for making a transaction. If Nonce is 0 and
// FixNonce is false, then nonce will be fetched from RPC call.
type TransactionConfig struct {
	Fee        string
	Nonce      int64 // nonce is changed to signed int for gomobile compatibility
	FixNonce   bool
	Attributes []byte
}

// DefaultTransactionConfig is the default TransactionConfig.
var DefaultTransactionConfig = TransactionConfig{
	Fee:        "0",
	Nonce:      0,
	FixNonce:   false,
	Attributes: nil,
}

// GetDefaultTransactionConfig returns the default rpc config with nil pointer
// fields set to default.
func GetDefaultTransactionConfig() *TransactionConfig {
	txnConf := DefaultTransactionConfig
	return &txnConf
}

// MergeClientConfig merges a given client config with the default client config
// recursively. Any non-zero value fields will override the default config.
func MergeClientConfig(conf *ClientConfig) (*ClientConfig, error) {
	merged := GetDefaultClientConfig()
	if conf != nil {
		err := mergo.Merge(merged, conf, mergo.WithOverride)
		if err != nil {
			return nil, err
		}
	}
	if merged.SeedRPCServerAddr.Len() == 0 {
		merged.SeedRPCServerAddr = nkngomobile.NewStringArray(DefaultSeedRPCServerAddr...)
	}
	return merged, nil
}

// MergeMessageConfig merges a given message config with the default message
// config recursively. Any non zero value fields will override the default
// config.
func MergeMessageConfig(base, conf *MessageConfig) (*MessageConfig, error) {
	merged := *base
	if conf != nil {
		err := mergo.Merge(&merged, conf, mergo.WithOverride)
		if err != nil {
			return nil, err
		}
	}
	return &merged, nil
}

// MergeDialConfig merges a given dial config with the default dial config
// recursively. Any non zero value fields will override the default config.
func MergeDialConfig(baseSessionConfig *ncp.Config, conf *DialConfig) (*DialConfig, error) {
	merged := GetDefaultDialConfig(baseSessionConfig)
	if conf != nil {
		err := mergo.Merge(merged, conf, mergo.WithOverride)
		if err != nil {
			return nil, err
		}
	}
	return merged, nil
}

// MergeWalletConfig merges a given wallet config with the default wallet config
// recursively. Any non-zero value fields will override the default config.
func MergeWalletConfig(conf *WalletConfig) (*WalletConfig, error) {
	merged := GetDefaultWalletConfig()
	if conf != nil {
		err := mergo.Merge(merged, conf, mergo.WithOverride)
		if err != nil {
			return nil, err
		}
	}
	if merged.SeedRPCServerAddr.Len() == 0 {
		merged.SeedRPCServerAddr = nkngomobile.NewStringArray(DefaultSeedRPCServerAddr...)
	}
	return merged, nil
}

// MergeTransactionConfig merges a given transaction config with the default
// transaction config recursively. Any non zero value fields will override the
// default config.
func MergeTransactionConfig(conf *TransactionConfig) (*TransactionConfig, error) {
	merged := GetDefaultTransactionConfig()
	if conf != nil {
		err := mergo.Merge(merged, conf, mergo.WithOverride)
		if err != nil {
			return nil, err
		}
	}
	return merged, nil
}
