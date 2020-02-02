package nkn

import (
	"math/rand"
	"time"

	"github.com/imdario/mergo"
	ncp "github.com/nknorg/ncp-go"
)

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
	"http://mainnet-seed-0041.nkn.org:30003",
	"http://mainnet-seed-0042.nkn.org:30003",
	"http://mainnet-seed-0043.nkn.org:30003",
	"http://mainnet-seed-0044.nkn.org:30003",
}

type ClientConfig struct {
	SeedRPCServerAddr       *StringArray
	MaxHoldingSeconds       int32
	MsgChanLen              int32
	BlockChanLen            int32
	ConnectRetries          int32
	MsgCacheExpiration      int32 // in millisecond
	MsgCacheCleanupInterval int32 // in millisecond
	WsHandshakeTimeout      int32 // in millisecond
	MinReconnectInterval    int32 // in millisecond
	MaxReconnectInterval    int32 // in millisecond
	MessageConfig           *MessageConfig
	SessionConfig           *ncp.Config
}

var defaultClientConfig = ClientConfig{
	MaxHoldingSeconds:       0,
	MsgChanLen:              1024,
	BlockChanLen:            1,
	ConnectRetries:          3,
	MsgCacheExpiration:      300000,
	MsgCacheCleanupInterval: 60000,
	WsHandshakeTimeout:      5000,
	MaxReconnectInterval:    60000,
	MessageConfig:           nil,
	SessionConfig:           nil,
}

func DefaultClientConfig() *ClientConfig {
	clientConf := defaultClientConfig
	clientConf.SeedRPCServerAddr = NewStringArray(DefaultSeedRPCServerAddr...)
	clientConf.MessageConfig = DefaultMessageConfig()
	clientConf.SessionConfig = DefaultSessionConfig()
	return &clientConf
}

type MessageConfig struct {
	Unencrypted       bool
	NoAck             bool
	MaxHoldingSeconds int32

	// for publish
	Offset int32
	Limit  int32
	TxPool bool
}

var defaultMessageConfig = MessageConfig{
	Unencrypted:       false,
	NoAck:             false,
	MaxHoldingSeconds: 0,
	Offset:            0,
	Limit:             1000,
	TxPool:            false,
}

func DefaultMessageConfig() *MessageConfig {
	messageConf := defaultMessageConfig
	return &messageConf
}

var defaultSessionConfig = ncp.Config{
	MTU: 1024,
}

func DefaultSessionConfig() *ncp.Config {
	sessionConf := defaultSessionConfig
	return &sessionConf
}

type DialConfig struct {
	DialTimeout   int32 //in millisecond
	SessionConfig *ncp.Config
}

var defaultDialConfig = DialConfig{
	DialTimeout:   0,
	SessionConfig: nil,
}

func DefaultDialConfig(baseSessionConfig *ncp.Config) *DialConfig {
	dialConf := defaultDialConfig
	sessionConfig := *baseSessionConfig
	dialConf.SessionConfig = &sessionConfig
	return &dialConf
}

type WalletConfig struct {
	SeedRPCServerAddr *StringArray
}

var defaultWalletConfig = WalletConfig{}

func DefaultWalletConfig() *WalletConfig {
	walletConf := defaultWalletConfig
	walletConf.SeedRPCServerAddr = NewStringArray(DefaultSeedRPCServerAddr...)
	return &walletConf
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

func (config *ClientConfig) GetRandomSeedRPCServerAddr() string {
	if len(config.SeedRPCServerAddr.Elems) == 0 {
		return ""
	}
	return config.SeedRPCServerAddr.Elems[rand.Intn(len(config.SeedRPCServerAddr.Elems))]
}

func (config *WalletConfig) GetRandomSeedRPCServerAddr() string {
	if len(config.SeedRPCServerAddr.Elems) == 0 {
		return ""
	}
	return config.SeedRPCServerAddr.Elems[rand.Intn(len(config.SeedRPCServerAddr.Elems))]
}

func MergeClientConfig(conf *ClientConfig) (*ClientConfig, error) {
	merged := DefaultClientConfig()
	if conf != nil {
		err := mergo.Merge(merged, conf, mergo.WithOverride)
		if err != nil {
			return nil, err
		}
	}
	return merged, nil
}

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

func MergeDialConfig(baseSessionConfig *ncp.Config, conf *DialConfig) (*DialConfig, error) {
	merged := DefaultDialConfig(baseSessionConfig)
	if conf != nil {
		err := mergo.Merge(merged, conf, mergo.WithOverride)
		if err != nil {
			return nil, err
		}
	}
	return merged, nil
}

func MergeWalletConfig(conf *WalletConfig) (*WalletConfig, error) {
	merged := DefaultWalletConfig()
	if conf != nil {
		err := mergo.Merge(merged, conf, mergo.WithOverride)
		if err != nil {
			return nil, err
		}
	}
	return merged, nil
}
