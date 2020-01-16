package nkn_sdk_go

import (
	"math/rand"
	"time"

	"github.com/imdario/mergo"
	"github.com/nknorg/ncp"
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

var DefaultClientConfig = ClientConfig{
	SeedRPCServerAddr:       DefaultSeedRPCServerAddr,
	MaxHoldingSeconds:       0,
	MsgChanLen:              1024,
	BlockChanLen:            1,
	ConnectRetries:          3,
	MsgCacheExpiration:      300 * time.Second,
	MsgCacheCleanupInterval: 60 * time.Second,
	WsHandshakeTimeout:      5 * time.Second,
	MaxReconnectInterval:    time.Minute,
	SessionConfig:           DefaultSessionConfig,
}

var DefaultWalletConfig = WalletConfig{
	SeedRPCServerAddr: DefaultSeedRPCServerAddr,
}

var DefaultSessionConfig = SessionConfig{
	SessionWindowSize:            4 << 20,
	MTU:                          1024,
	InitialConnectionWindowSize:  16,
	MaxConnectionWindowSize:      256,
	MinConnectionWindowSize:      1,
	MaxAckSeqListSize:            32,
	NonStream:                    false,
	FlushInterval:                10 * time.Millisecond,
	CloseDelay:                   100 * time.Millisecond,
	InitialRetransmissionTimeout: 5 * time.Second,
	MaxRetransmissionTimeout:     10 * time.Second,
	SendAckInterval:              50 * time.Millisecond,
	CheckTimeoutInterval:         50 * time.Millisecond,
	DialTimeout:                  0,
}

type ClientConfig struct {
	SeedRPCServerAddr       []string
	MaxHoldingSeconds       int32
	MsgChanLen              int32
	BlockChanLen            int32
	ConnectRetries          int32
	MsgCacheExpiration      time.Duration
	MsgCacheCleanupInterval time.Duration
	WsHandshakeTimeout      time.Duration
	MinReconnectInterval    time.Duration
	MaxReconnectInterval    time.Duration
	SessionConfig           SessionConfig
}

type WalletConfig struct {
	SeedRPCServerAddr []string
}

type SessionConfig ncp.Config

func init() {
	rand.Seed(time.Now().UnixNano())
}

func (config *ClientConfig) GetRandomSeedRPCServerAddr() string {
	if len(config.SeedRPCServerAddr) == 0 {
		return ""
	}
	return config.SeedRPCServerAddr[rand.Intn(len(config.SeedRPCServerAddr))]
}

func (config *WalletConfig) GetRandomSeedRPCServerAddr() string {
	if len(config.SeedRPCServerAddr) == 0 {
		return ""
	}
	return config.SeedRPCServerAddr[rand.Intn(len(config.SeedRPCServerAddr))]
}

func MergedClientConfig(conf []ClientConfig) (*ClientConfig, error) {
	merged := DefaultClientConfig
	if len(conf) > 0 {
		err := mergo.Merge(&merged, &conf[0], mergo.WithOverride)
		if err != nil {
			return nil, err
		}
	}
	return &merged, nil
}

func MergedWalletConfig(conf []WalletConfig) (*WalletConfig, error) {
	merged := DefaultWalletConfig
	if len(conf) > 0 {
		err := mergo.Merge(&merged, &conf[0], mergo.WithOverride)
		if err != nil {
			return nil, err
		}
	}
	return &merged, nil
}

func MergedSessionConfig(conf *SessionConfig) (*SessionConfig, error) {
	merged := DefaultSessionConfig
	if conf != nil {
		err := mergo.Merge(&merged, conf, mergo.WithOverride)
		if err != nil {
			return nil, err
		}
	}
	return &merged, nil
}
