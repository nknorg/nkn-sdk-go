package nkn_sdk_go

import (
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"math/rand"
	"time"

	"github.com/nknorg/nkn/api/httpjson/client"
)

var seedList = []string{
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
