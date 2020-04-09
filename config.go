package nkn

import (
	"math/rand"
	"time"

	"github.com/imdario/mergo"
	ncp "github.com/nknorg/ncp-go"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// DefaultSeedRPCServerAddr is the default seed rpc server address list.
var DefaultSeedRPCServerAddr = []string{
	"http://mainnet-seed-0001.nkn.org:30003",
	"http://mainnet-seed-0002.nkn.org:30003",
	"http://mainnet-seed-0003.nkn.org:30003",
	"http://mainnet-seed-0004.nkn.org:30003",
	"http://mainnet-seed-0005.nkn.org:30003",
	"http://mainnet-seed-0006.nkn.org:30003",
	"http://mainnet-seed-0007.nkn.org:30003",
	"http://mainnet-seed-0008.nkn.org:30003",
	"http://mainnet-seed-0009.nkn.org:30003",
	"http://mainnet-seed-0010.nkn.org:30003",
	"http://mainnet-seed-0011.nkn.org:30003",
	"http://mainnet-seed-0012.nkn.org:30003",
	"http://mainnet-seed-0013.nkn.org:30003",
	"http://mainnet-seed-0014.nkn.org:30003",
	"http://mainnet-seed-0015.nkn.org:30003",
	"http://mainnet-seed-0016.nkn.org:30003",
	"http://mainnet-seed-0017.nkn.org:30003",
	"http://mainnet-seed-0018.nkn.org:30003",
	"http://mainnet-seed-0019.nkn.org:30003",
	"http://mainnet-seed-0020.nkn.org:30003",
	"http://mainnet-seed-0021.nkn.org:30003",
	"http://mainnet-seed-0022.nkn.org:30003",
	"http://mainnet-seed-0023.nkn.org:30003",
	"http://mainnet-seed-0024.nkn.org:30003",
	"http://mainnet-seed-0025.nkn.org:30003",
	"http://mainnet-seed-0026.nkn.org:30003",
	"http://mainnet-seed-0027.nkn.org:30003",
	"http://mainnet-seed-0028.nkn.org:30003",
	"http://mainnet-seed-0029.nkn.org:30003",
	"http://mainnet-seed-0030.nkn.org:30003",
	"http://mainnet-seed-0031.nkn.org:30003",
	"http://mainnet-seed-0032.nkn.org:30003",
	"http://mainnet-seed-0033.nkn.org:30003",
	"http://mainnet-seed-0034.nkn.org:30003",
	"http://mainnet-seed-0035.nkn.org:30003",
	"http://mainnet-seed-0036.nkn.org:30003",
	"http://mainnet-seed-0037.nkn.org:30003",
	"http://mainnet-seed-0038.nkn.org:30003",
	"http://mainnet-seed-0039.nkn.org:30003",
	"http://mainnet-seed-0040.nkn.org:30003",
}

// ClientConfig is the client configuration.
type ClientConfig struct {
	SeedRPCServerAddr       *StringArray   // Seed RPC server address that client uses to find its node and make RPC requests (e.g. get subscribers).
	MsgChanLen              int32          // Channel length for received but unproccessed messages.
	ConnectRetries          int32          // Connnect to node retries (including the initial connect). 0 means unlimited retries.
	MsgCacheExpiration      int32          // Message cache expiration in millisecond for response channel, multiclient message id deduplicate, etc.
	MsgCacheCleanupInterval int32          // Message cache cleanup interval in millisecond.
	WsHandshakeTimeout      int32          // WebSocket handshake timeout in millisecond.
	WsWriteTimeout          int32          // WebSocket write timeout in millisecond.
	MinReconnectInterval    int32          // Min reconnect interval in millisecond.
	MaxReconnectInterval    int32          // Max reconnect interval in millisecond.
	MessageConfig           *MessageConfig // Default message config of the client if per-message config is not provided.
	SessionConfig           *ncp.Config    // Default session config of the client if per-session config is not provided.
}

// DefaultClientConfig is the default client config.
var DefaultClientConfig = ClientConfig{
	SeedRPCServerAddr:       nil,
	MsgChanLen:              1024,
	ConnectRetries:          3,
	MsgCacheExpiration:      300000,
	MsgCacheCleanupInterval: 60000,
	WsHandshakeTimeout:      5000,
	WsWriteTimeout:          10000,
	MinReconnectInterval:    1000,
	MaxReconnectInterval:    64000,
	MessageConfig:           nil,
	SessionConfig:           nil,
}

// GetDefaultClientConfig returns the default client config with nil pointer
// fields set to default.
func GetDefaultClientConfig() *ClientConfig {
	clientConf := DefaultClientConfig
	clientConf.SeedRPCServerAddr = NewStringArray(DefaultSeedRPCServerAddr...)
	clientConf.MessageConfig = GetDefaultMessageConfig()
	clientConf.SessionConfig = GetDefaultSessionConfig()
	return &clientConf
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

// WalletConfig is the wallet configuration.
type WalletConfig struct {
	SeedRPCServerAddr *StringArray
	Password          string
	IV                []byte
	MasterKey         []byte
}

// DefaultWalletConfig is the default wallet configuration.
var DefaultWalletConfig = WalletConfig{
	SeedRPCServerAddr: nil,
}

// GetDefaultWalletConfig returns the default wallet config with nil pointer
// fields set to default.
func GetDefaultWalletConfig() *WalletConfig {
	walletConf := DefaultWalletConfig
	walletConf.SeedRPCServerAddr = NewStringArray(DefaultSeedRPCServerAddr...)
	return &walletConf
}

// RPCConfig is the rpc call configuration.
type RPCConfig struct {
	SeedRPCServerAddr *StringArray
}

// DefaultRPCConfig is the default rpc configuration.
var DefaultRPCConfig = RPCConfig{
	SeedRPCServerAddr: nil,
}

// GetDefaultRPCConfig returns the default rpc config with nil pointer fields
// set to default.
func GetDefaultRPCConfig() *RPCConfig {
	rpcConf := DefaultRPCConfig
	rpcConf.SeedRPCServerAddr = NewStringArray(DefaultSeedRPCServerAddr...)
	return &rpcConf
}

// GetRandomSeedRPCServerAddr returns a random seed rpc server address from the
// client config.
func (config *ClientConfig) GetRandomSeedRPCServerAddr() string {
	if len(config.SeedRPCServerAddr.Elems) == 0 {
		return ""
	}
	return config.SeedRPCServerAddr.Elems[rand.Intn(len(config.SeedRPCServerAddr.Elems))]
}

// GetRandomSeedRPCServerAddr returns a random seed rpc server address from the
// wallet config.
func (config *WalletConfig) GetRandomSeedRPCServerAddr() string {
	if len(config.SeedRPCServerAddr.Elems) == 0 {
		return ""
	}
	return config.SeedRPCServerAddr.Elems[rand.Intn(len(config.SeedRPCServerAddr.Elems))]
}

// GetRandomSeedRPCServerAddr returns a random seed rpc server address from the
// rpc config.
func (config *RPCConfig) GetRandomSeedRPCServerAddr() string {
	if len(config.SeedRPCServerAddr.Elems) == 0 {
		return ""
	}
	return config.SeedRPCServerAddr.Elems[rand.Intn(len(config.SeedRPCServerAddr.Elems))]
}

// MergeClientConfig merges a given client config with the default client config
// recursively. Any non zero value fields will override the default config.
func MergeClientConfig(conf *ClientConfig) (*ClientConfig, error) {
	merged := GetDefaultClientConfig()
	if conf != nil {
		err := mergo.Merge(merged, conf, mergo.WithOverride)
		if err != nil {
			return nil, err
		}
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
// recursively. Any non zero value fields will override the default config.
func MergeWalletConfig(conf *WalletConfig) (*WalletConfig, error) {
	merged := GetDefaultWalletConfig()
	if conf != nil {
		err := mergo.Merge(merged, conf, mergo.WithOverride)
		if err != nil {
			return nil, err
		}
	}
	return merged, nil
}
