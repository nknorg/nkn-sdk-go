package nkn_sdk_go

import (
	cryptorand "crypto/rand"
	"encoding/json"
	"math/big"
	"math/rand"
	"time"

	"github.com/nknorg/nkn/api/httpjson/client"
	"github.com/pkg/errors"
)

var seedList = []string{
	"http://mainnet-seed-0001.nkn.org:30003",
	"http://mainnet-seed-0002.nkn.org:30003",
	"http://mainnet-seed-0003.nkn.org:30003",
	"http://mainnet-seed-0004.nkn.org:30003",
	"http://mainnet-seed-0005.nkn.org:30003",
}

var seedRPCServerAddr string

func Init() {
	if seedRPCServerAddr == "" {
		rand.Seed(time.Now().UnixNano())
		rand.Shuffle(len(seedList), func(i int, j int) {
			seedList[i], seedList[j] = seedList[j], seedList[i]
		})
		seedRPCServerAddr = seedList[0]
	}
}

func call(address string, action string, params map[string]interface{}, result interface{}) (error, int32) {
	data, err := client.Call(address, action, 0, params)
	resp := make(map[string]*json.RawMessage)
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return err, -1
	}
	if resp["error"] != nil {
		error := make(map[string]interface{})
		err := json.Unmarshal(*resp["error"], &error)
		if err != nil {
			return err, -1
		}
		var detailsCode int32
		if resp["details"] != nil {
			details := make(map[string]interface{})
			err := json.Unmarshal(*resp["details"], &details)
			if err != nil {
				return err, -1
			}
			detailsCode = int32(details["code"].(float64))
		} else {
			detailsCode = -1
		}
		return errors.New(error["message"].(string)), detailsCode
	}

	err = json.Unmarshal(*resp["result"], result)
	if err != nil {
		return err, 0
	}
	return nil, 0
}

func randUint32() uint32 {
	max := big.NewInt(4294967296)
	for {
		result, err := cryptorand.Int(cryptorand.Reader, max)
		if err != nil {
			continue
		}
		return uint32(result.Uint64())
	}
}

func randUint64() uint64 {
	max := new(big.Int).SetUint64(18446744073709551615)
	max.Add(max, big.NewInt(1))
	for {
		result, err := cryptorand.Int(cryptorand.Reader, max)
		if err != nil {
			continue
		}
		return result.Uint64()
	}
}
