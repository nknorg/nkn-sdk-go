package nkn_sdk_go

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"time"

	"github.com/nknorg/nkn/api/httpjson/client"
	"github.com/nknorg/nkn/common"
	"github.com/nknorg/nkn/crypto"
	"github.com/pkg/errors"
)

var SeedList = []string{
	"http://testnet-node-0001.nkn.org:30003",
	"http://testnet-node-0002.nkn.org:30003",
	"http://testnet-node-0003.nkn.org:30003",
	"http://testnet-node-0004.nkn.org:30003",
	"http://testnet-node-0005.nkn.org:30003",
	"http://testnet-node-0006.nkn.org:30003",
}

var SeedRPCServerAddr string
var AssetId common.Uint256

func Init() {
	if SeedRPCServerAddr == "" {
		rand.Seed(time.Now().UnixNano())
		rand.Shuffle(len(SeedList), func(i int, j int) {
			SeedList[i], SeedList[j] = SeedList[j], SeedList[i]
		})
		SeedRPCServerAddr = SeedList[0]
	}

	tmp, _ := common.HexStringToBytesReverse("4945ca009174097e6614d306b66e1f9cb1fce586cb857729be9e1c5cc04c9c02")
	if err := AssetId.Deserialize(bytes.NewReader(tmp)); err != nil {
		panic(err)
	}

	crypto.SetAlg("")
}

func call(action string, params map[string]interface{}, result interface{}) (error, int32) {
	data, err := client.Call(SeedRPCServerAddr, action, 0, params)
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
