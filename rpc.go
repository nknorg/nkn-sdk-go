package nkn

import (
	"encoding/json"
	"errors"

	"github.com/nknorg/nkn/api/httpjson/client"
)

// Node struct contains the information of the node that a client connects to.
type Node struct {
	Address   string `json:"addr"`
	PublicKey string `json:"pubkey"`
	ID        string `json:"id"`
}

// Subscription contains the information of a subscriber to a topic.
type Subscription struct {
	Meta      string `json:"meta"`
	ExpiresAt int32  `json:"expiresAt"` // Changed to signed int for gomobile compatibility
}

type balance struct {
	Amount string `json:"amount"`
}

type nonce struct {
	Nonce         uint64 `json:"nonce"`
	NonceInTxPool uint64 `json:"nonceInTxPool"`
}

type registrantInfo struct {
	Registrant string `json:"registrant"`
	ExpiresAt  uint32 `json:"expiresAt"`
}

func call(address string, action string, params map[string]interface{}, result interface{}) (int32, error) {
	data, err := client.Call(address, action, 0, params)
	if err != nil {
		return -1, err
	}
	resp := make(map[string]*json.RawMessage)
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return -1, err
	}
	if resp["error"] != nil {
		errResp := &struct {
			Code    int32
			Message string
			Data    string
		}{}
		err := json.Unmarshal(*resp["error"], &errResp)
		if err != nil {
			return -1, err
		}
		code := errResp.Code
		if code < 0 {
			code = -1
		}
		msg := errResp.Message
		if len(errResp.Data) > 0 {
			msg += ": " + errResp.Data
		}
		return code, errors.New(msg)
	}

	err = json.Unmarshal(*resp["result"], result)
	if err != nil {
		return 0, err
	}
	return 0, nil
}
